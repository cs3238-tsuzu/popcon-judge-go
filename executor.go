package main

import "github.com/docker/docker/pkg/stdcopy"
import "github.com/docker/engine-api/types"
import "github.com/docker/engine-api/types/container"
import "golang.org/x/net/context"
import "errors"
import "bytes"
import "strconv"
import "strings"
import "archive/tar"

type LengthLimitedString struct {
	dst *string
	Len int
}

func (lls LengthLimitedString) Write(b []byte) (int, error) {
	if lls.dst == nil {
		return len(b), nil
	}

	if len(b) + len(*lls.dst) > lls.Len {
		*lls.dst += string(b[:lls.Len - len(*lls.dst)])
	}else {
		*lls.dst += string(b)
	}

	return len(b), nil
}

type Executor struct {
	Name string
	Mem  int64
	Time int64
	Cgr  Cgroup
}

type ExecStatus int

const (
	ExecFinished            ExecStatus = 0
	ExecTimeLimitExceeded   ExecStatus = 1
	ExecMemoryLimitExceeded ExecStatus = 2
	ExecError               ExecStatus = 3
)

type ExecResult struct {
	Status   ExecStatus
	Time     int64
	Mem      int64
	ExitCode int
	Stdout   string
	Stderr   string
}

func (e *Executor) Run(input string) ExecResult {
	cg := e.Cgr
	memc := cg.getSubsys("memory")

	stdinErr := make(chan error, 1)
	stdoutErr := make(chan error, 1)
	stderrErr := make(chan error, 1)

	attachment := func(opt types.ContainerAttachOptions, done chan<- error, out *string) {
		ctx := context.Background()
		hijack, err := cli.ContainerAttach(ctx, e.Name, opt)

		if err != nil {
			panic(err)
		}
		done <- nil
		if opt.Stdin {
			hijack.Conn.Write([]byte(input))
			hijack.CloseWrite()
			hijack.Close()
			hijack.Conn.Close()

			done <- nil
			return
		}

		lls := LengthLimitedString{out, 100 * 1024 * 1024}

		if opt.Stdout {
			_, err = stdcopy.StdCopy(lls, LengthLimitedString{nil, 0}, hijack.Reader)

		}else {
			_, err = stdcopy.StdCopy(LengthLimitedString{nil, 0}, lls, hijack.Reader)
		}
		done <- err
	}

	var stdout, stderr string

	go attachment(types.ContainerAttachOptions{Stream: true, Stdout: true}, stdoutErr, &stdout)
	go attachment(types.ContainerAttachOptions{Stream: true, Stderr: true}, stderrErr, &stderr)
	go attachment(types.ContainerAttachOptions{Stream: true, Stdin: true}, stdinErr, nil)

	<-stdinErr
	<-stdoutErr
	<-stderrErr

	ctx := context.Background()
	err := cli.ContainerStart(ctx, e.Name, types.ContainerStartOptions{})

	if err != nil {
		return ExecResult{ExecError, 0, 0, 0, "", "Failed to start a container. " + err.Error()}
	}

	<-stdinErr
	<-stdoutErr
	<-stderrErr

	usedMem, err := memc.getValInt("memory.memsw.max_usage_in_bytes")

	if usedMem >= e.Mem {
		return ExecResult{ExecMemoryLimitExceeded, 0, 0, 0, "", ""}
	}

	rc, _, err := cli.CopyFromContainer(ctx, e.Name, "/tmp/time.txt")

	if err != nil {
		cli.ContainerKill(ctx, e.Name, "SIGKILL")

		return ExecResult{ExecError, 0, 0, 0, "", "Failed to read the execution time. " + err.Error()}
	}

	tarStream := tar.NewReader(rc)
	tarStream.Next()

	buf := new(bytes.Buffer)
	buf.ReadFrom(tarStream)
	exitCode := 0

	lines := strings.Split(buf.String(), "\n")

	if len(lines) == 3 {
		for _, str := range strings.Split(lines[0], " ") {
			code, err := strconv.ParseInt(str, 10, 32)

			if err == nil {
				exitCode = 128 + int(code)

				goto loop
			}
		}
		return ExecResult{ExecError, 0, 0, 0, "", "Failed to parse the result."}

	loop:
	}

	var execTime int64
	if len(lines) >= 2 {
		var arrRes []string

		if len(lines) == 2 {
			arrRes = strings.Split(lines[0], " ")
		}else {
			arrRes = strings.Split(lines[1], " ")
		}

		if len(arrRes) != 2 {

			return ExecResult{ExecError, 0, 0, 0, "", "Failed to parse the result."}
		}

		execSec, err := strconv.ParseFloat(arrRes[0], 64)

		if err != nil {
			return ExecResult{ExecError, 0, 0, 0, "", "Failed to parse the execution result."}
		}

		execTime = int64(execSec * 1000)

		exit64, err := strconv.ParseInt(arrRes[1], 10, 32)

		if err != nil {
			return ExecResult{ExecError, 0, 0, 0, "", "Failed to parse the exit code."}
		}

		if exitCode == 0 {
			exitCode = int(exit64)
		}

		if execSec*1000 > float64(e.Time) {
			cli.ContainerKill(ctx, e.Name, "SIGKILL")

			return ExecResult{ExecTimeLimitExceeded, 0, 0, 0, "", ""}
		}

	}

	return ExecResult{ExecFinished, execTime, usedMem, exitCode, stdout, stderr}
}

func (e *Executor) CopyToContainer(root string, files []struct{name string; content string}) error {
	buf := new(bytes.Buffer)

	tw := tar.NewWriter(buf)

	for i := range files {
		err := tw.WriteHeader(&tar.Header{Name: files[i].name, Mode: 0777, Size: int64(len(files[i].content))})

		if err != nil {
			return err
		}

		_, err = tw.Write([]byte(files[i].content))

		if err != nil {
			return err
		}
	}

	return cli.CopyToContainer(context.Background(), e.Name, root, buf, types.CopyToContainerOptions{AllowOverwriteDirWithFile: true})
}

func (e *Executor) Delete() error {
	err := e.Cgr.Delete()
	err2 := cli.ContainerRemove(context.Background(), e.Name, types.ContainerRemoveOptions{Force: true})

	errstr := ""
	if err != nil {
		errstr += err.Error()
	}
	if err2 != nil {
		errstr += err2.Error()
	}

	if errstr == "" {
		return nil
	} else {
		return errors.New(errstr)
	}
}

func NewExecutor(name string, mem int64, time int64, cmd []string, img string, binds []string) (*Executor, error) {
	ctx := context.Background()

	cg := NewCgroup(name)

	err := cg.addSubsys("memory").Modify()

	if err != nil {
		return nil, errors.New("Failed to create a cgroup")
	}

	err = cg.getSubsys("memory").setValInt(mem, "memory.limit_in_bytes")

	if err != nil {
		cg.Delete()

		return nil, errors.New("Failed to set memory.limit_in_bytes")
	}

	err = cg.getSubsys("memory").setValInt(mem, "memory.memsw.limit_in_bytes")

	if err != nil {
		cg.Delete()

		return nil, errors.New("Failed to set memory.memsw.limit_in_bytes")
	}

	cfg := container.Config{}

	cfg.Tty = false
	cfg.AttachStderr = true
	cfg.AttachStdout = true
	cfg.AttachStdin = true
	cfg.OpenStdin = true
	cfg.StdinOnce = true
	cfg.Image = img
	cfg.Hostname = "localhost"

	var timer = []string{"/usr/bin/time", "-q", "-f", "%e %x", "-o", "/tmp/time.txt", "/usr/bin/timeout", strconv.FormatInt((time+1000)/1000, 10), "/usr/bin/sudo", "-u", "nobody"}

	newCmd := make([]string, 0, len(cmd)+len(timer))

	for i := range timer {
		newCmd = append(newCmd, timer[i])
	}

	for i := range cmd {
		newCmd = append(newCmd, cmd[i])
	}

	cfg.Cmd = newCmd

	hcfg := container.HostConfig{}

	hcfg.CPUQuota = int64(1000 * settingData.CPUUsage)
	hcfg.CPUPeriod = 100000
	hcfg.NetworkMode = "none"
	hcfg.Binds = binds
	hcfg.CgroupParent = "/" + name

	_, err = cli.ContainerCreate(ctx, &cfg, &hcfg, nil, name)

	if err != nil {
		cg.Delete()

		return nil, errors.New("Failed to create a container " + err.Error())
	}

	return &Executor{name, mem, time, cg}, nil
}
