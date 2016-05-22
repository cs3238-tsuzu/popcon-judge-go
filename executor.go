package main

import "github.com/docker/engine-api/types"
import "github.com/docker/engine-api/types/container"
import "golang.org/x/net/context"
import "errors"
import "time"
import "fmt"
import "bytes"
//import "os"
//import "strconv"
//import "os/exec"

type Executor struct {
	Name string
	Mem  int64
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

func (e *Executor) Run(msTime int64, input string) ExecResult {
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

		var buf bytes.Buffer
		for {
			b := make([]byte, 128)

			size, err := hijack.Reader.Read(b)

			// Output Size Limitation
			if out != nil && len(*out) < 100*1024*1024 {
				buf.Write(b[0:size])

				if buf.Len() >= 8 {
					var size uint32
					bin := buf.Bytes()

					for i, v := range bin[4:8] {
						shift := uint32((3 - i) * 8)

						size |= uint32(v) << shift
					}

					if buf.Len() >= int(size+8) {
						*out += string(bin[8 : size+8])
						buf.Reset()
						buf.Write(bin[size+8:])
					}
				}
			}

			if err != nil {
				if err.Error() == "EOF" {
					done <- nil
				} else {
					done <- err
				}

				return
			}
		}
	}

	var stdout, stderr string

	go attachment(types.ContainerAttachOptions{Stream: true, Stdout: true}, stdoutErr, &stdout)
	go attachment(types.ContainerAttachOptions{Stream: true, Stderr: true}, stderrErr, &stderr)
	go attachment(types.ContainerAttachOptions{Stream: true, Stdin: true}, stdinErr, nil)

	timerChan := make(chan bool, 1)
	execTimeChan := make(chan int64, 1)

	go func(done <-chan bool, result chan<- int64) {
		var start int64 = 0
		var chil string

		for {
			select {
			case <-done:
				if start == 0 {
					result <- 1
				} else {
					result <- time.Now().UnixNano()/int64(time.Millisecond) - start
				}
				break
			default:
			}

			if chil == "" {
				list, _ := memc.listChildren()

				if len(list) != 0 {
					chil = list[0]
				}
			} else {
				val, err := memc.getVal(chil + "/tasks")

				if err != nil {
					if start == 0 {
						result <- 1
					} else {
						result <- time.Now().UnixNano()/int64(time.Millisecond) - start
					}
				} else {
					if start == 0 {
						if len(*val) != 0 {
							start = time.Now().UnixNano() / int64(time.Millisecond)
						}
					} else {
						if len(*val) == 0 {
							result <- time.Now().UnixNano()/int64(time.Millisecond) - start

							return
						} else {

							t := time.Now().UnixNano()/int64(time.Millisecond) - start
							if t > msTime {
								fmt.Println("Timed out")
								result <- t

								return
							}
						}
					}
				}
			}

			time.Sleep(time.Nanosecond * 100)
		}
	}(timerChan, execTimeChan)

	<-stdinErr
	<-stdoutErr
	<-stderrErr
	<-stdinErr

	ctx := context.Background()
	err := cli.ContainerStart(ctx, e.Name)

	if err != nil {
		timerChan <- false

		return ExecResult{ExecError, 0, 0, 0, "", "Failed to start a container"}
	}

	execTime := <-execTimeChan

	if execTime > msTime {
        // Kill process in the container
		/*proc, err := cli.ContainerTop(ctx, e.Name, []string{})

				if err == nil {
					pidIdx := -1

					for x := range proc.Titles {
						if proc.Titles[x] == "PID" {
							pidIdx = x
						}
					}

					if pidIdx != -1 {
		    	    	for x := range proc.Processes {
		                    pid, err := strconv.Atoi(proc.Processes[x][pidIdx])

		                    if err != nil {
		                        continue
		                    }

		                    p, err := os.FindProcess(pid)

		                    if err != nil {
		                        continue
		                    }
		                    p.Release()
						}
					}

				}*/
//		exec.Command("docker", "kill", "Hello").Output()
        
        cli.ContainerKill(ctx, e.Name, "SIGKILL")

		return ExecResult{ExecTimeLimitExceeded, 0, 0, 0, "", ""}
	}

	<-stdoutErr
	<-stderrErr

	usedMem, err := memc.getValInt("memory.max_usage_in_bytes")

	if usedMem >= e.Mem {
		return ExecResult{ExecMemoryLimitExceeded, 0, 0, 0, "", ""}
	}

	insp, err := cli.ContainerInspect(ctx, e.Name)

	exitCode := 0
	if err == nil && insp.State != nil {
		exitCode = insp.State.ExitCode
	}

	return ExecResult{ExecFinished, execTime, usedMem, exitCode, stdout, stderr}
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

func NewExecutor(name string, mem int64, cmd []string, img string, binds []string, user string) (*Executor, error) {
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
	cfg.User = user
	cfg.Image = img
	cfg.Cmd = cmd

	hcfg := container.HostConfig{}

	hcfg.NetworkMode = "none"
	hcfg.Binds = binds
	hcfg.CgroupParent = "/" + name

	_, err = cli.ContainerCreate(ctx, &cfg, &hcfg, nil, name)

	if err != nil {
		cg.Delete()

		return nil, errors.New("Failed to create a container " + err.Error())
	}

	return &Executor{name, mem, cg}, nil
}
