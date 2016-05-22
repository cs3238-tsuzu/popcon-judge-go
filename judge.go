package main

import "os"
import "github.com/docker/engine-api/client"
import "math/rand"
import "os/exec"
import "strconv"
import "fmt"

var cli *client.Client

type ExecRequest struct {
	Image string
	Cmd []string
	SourceFileName string
}

type Judge struct {
	Code string
	Compile *ExecRequest
	Exec ExecRequest
	Time int64
	Mem  int64
	TCCount int // The number of test cases
}

type JudgeResult int

const (
	Accepted            JudgeResult = 0
	WrongAnswer         JudgeResult = 1
	CompileError        JudgeResult = 2
	TimeLimitExceeded   JudgeResult = 3
	MemoryLimitExceeded JudgeResult = 4
	RuntimeError        JudgeResult = 5
	InternalError       JudgeResult = 6
	Judging             JudgeResult = 7
	CompileTimeLimitExceeded JudgeResult = 8
	CompileMemoryLimitExceeded JudgeResult = 9
)

type JudgeStatus struct {
	Case     *string
	JR       JudgeResult
	Mem      int64
	Time     int64
	Msg      *string
}

func CreateInternalError(msg string) JudgeStatus {
	return JudgeStatus{nil, InternalError, 0, 0, &msg}
}

const BASE_RAND_STRING = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"

func RandomName() string {
	res := make([]byte, 0, 32)
	for i := 0; i < 32; i++ {
		res = append(res, BASE_RAND_STRING[rand.Intn(len(BASE_RAND_STRING))])
	}
	
	return string(res)
}

func (j *Judge) Run(ch chan<- JudgeStatus, tests <-chan struct {
	Name string
	In  string
	Out string
}) {
	// Close a channel to send results of judging
	defer close(ch)
	
	// Identity
	id := RandomName()

	// Working Directory
	path := workingDirectory + "/" + id

	fmt.Println(path)

	err := os.Mkdir(path, 0664)

	if err != nil {
		ch <- CreateInternalError("Failed to create a directory." + err.Error())

		return
	}
	
	defer os.RemoveAll(path)
	
	fp, err := os.Create(path + "/" + j.Compile.SourceFileName)
	
	if err != nil {
		ch <- CreateInternalError("Failed to create source file." + err.Error())

		return
	}
	
	l, err := fp.Write([]byte(j.Code))
	
	if err != nil {
		ch <- CreateInternalError("Failed to write your code on your file." + err.Error())

		return
	}

	if l != len(j.Code) {
		ch <- CreateInternalError("Failed to write your code on your file.")

		return
	}
	
	// User
	_, err = exec.Command("useradd", "--no-create-home", id).Output()
	
	if err != nil {
		ch <- CreateInternalError("Failed to create a directory to build your code." + err.Error())
		
		return
	}
	
	defer exec.Command("userdel", id)
	
	// Compile
	if j.Compile != nil {
		exe, err := NewExecutor(id, 512 * 1024 * 1024, j.Compile.Cmd, j.Compile.Image, []string{path + ":" + "/work"}, id)
		
		if err != nil {
			ch <- CreateInternalError("Failed to create a Docker container to compile your code." + err.Error())

			return
		}
		
		res := exe.Run(10000, "")
		
		exe.Delete()
		if res.Status != ExecFinished {
			switch res.Status {
			case ExecError:
				ch <- CreateInternalError("Failed to execute a compiler." + res.Stderr)
				
				return
			case ExecMemoryLimitExceeded:
				ch <- JudgeStatus{JR: CompileMemoryLimitExceeded}
				
				return
			case ExecTimeLimitExceeded:
				ch <- JudgeStatus{JR: CompileTimeLimitExceeded}
				
				return
			}
		}
		
		if res.ExitCode != 0 {
			msg := res.Stdout + res.Stderr
			ch <- JudgeStatus{JR: CompileError, Msg: &msg}
			
			return
		}
	}
	
	exe, err := NewExecutor(id, j.Mem, j.Exec.Cmd, j.Exec.Image, []string{path + ":" + "/work:ro"}, id)
	
	if err != nil {
		ch <- CreateInternalError("Failed to create a Docker container to judge." + err.Error())

		return
	}
	
	defer exe.Delete()
	
	tcCounter := 0
	for tc, res := <-tests; res; tc, res = <-tests {
		res := exe.Run(j.Time, tc.In)
		
		if res.Status != ExecFinished {
			switch res.Status {
			case ExecError:
				msg := "Failed to execute your code." + res.Stderr
				ch <- JudgeStatus{Case: &tc.Name, JR: InternalError, Msg: &msg}
			case ExecMemoryLimitExceeded:
				ch <- JudgeStatus{Case: &tc.Name, JR: MemoryLimitExceeded}
			case ExecTimeLimitExceeded:
				ch <- JudgeStatus{Case: &tc.Name, JR: TimeLimitExceeded}
			}
		}else {
			if res.ExitCode != 0 {
				ch <- JudgeStatus{Case: &tc.Name, JR: RuntimeError}
			}else {
				if res.Stdout == tc.Out {
					ch <- JudgeStatus{Case: &tc.Name, JR: Accepted}
				}else {
					ch <- JudgeStatus{Case: &tc.Name, JR: WrongAnswer}
				}
			}
		}
		
		tcCounter++
		
		msg := strconv.FormatInt(int64(tcCounter), 10) + "/" + strconv.FormatInt(int64(j.TCCount), 10)
		ch <- JudgeStatus{JR: Judging, Msg: &msg}
	}
	
}
