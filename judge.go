package main

import "os"
import "github.com/docker/engine-api/client"
import "math/rand"
import "os/exec"
import "strconv"
import "github.com/seehuhn/mt19937"
import "time"
import "os/user"
import "fmt"
import "math"

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
	MemoryLimitExceeded        JudgeResult = 2
	TimeLimitExceeded   JudgeResult = 3
	RuntimeError JudgeResult = 4
	InternalError        JudgeResult = 5
	Judging       JudgeResult = 6
	CompileError             JudgeResult = 7
	CompileTimeLimitExceeded JudgeResult = 8
	CompileMemoryLimitExceeded JudgeResult = 9
)

const JudgeResultToStr []string = []string{"Accepted", "WrongAnswer", "MemoryLimitExceeded", "TimeLimitExceeded", "RuntimeError", "InternalError", "Judging", "CompileError", "CompileTimeLimitExceeded", "CompileMemoryLimitExceeded"}

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
	rng := rand.New(mt19937.New())
	rng.Seed(time.Now().UnixNano())
	
	res := make([]byte, 0, 32)
	for i := 0; i < 32; i++ {
		res = append(res, BASE_RAND_STRING[rng.Intn(len(BASE_RAND_STRING))])
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
	
	// User
	_, err := exec.Command("useradd", "--no-create-home", id).Output()
	
	if err != nil {
		ch <- CreateInternalError("Failed to create a directory to build your code. " + err.Error())
		
		return
	}
	
	uid, err := user.Lookup(id)
	
	if err != nil {
		ch <- CreateInternalError("Failed to look up a user. " + err.Error())
		
		return
	}
	
	uidInt, err := strconv.ParseInt(uid.Uid, 10, 64)
	if err != nil {
		ch <- CreateInternalError("Failed to parseInt uid. " + err.Error())
		
		return
	}

	gidInt, err := strconv.ParseInt(uid.Gid, 10, 64)
	if err != nil {
		ch <- CreateInternalError("Failed to parseInt gid. " + err.Error())
		
		return
	}
	
	defer exec.Command("userdel", id).Output()
	
	// Working Directory
	path := workingDirectory + "/" + id

	err = os.Mkdir(path, 0777)

	if err != nil {
		ch <- CreateInternalError("Failed to create a directory. " + err.Error())

		return
	}
	
	defer os.RemoveAll(path)

	uidInt = uidInt * gidInt / gidInt
	err = os.Chown(path, int(uidInt), int(gidInt))

	if err != nil {
		ch <- CreateInternalError("Failed to chown the directory. " + err.Error())
		
		return
	}
	
	err = os.Chmod(path, 0777)
	
	if err != nil {
		ch <- CreateInternalError("Failed to chmod the directory. " + err.Error())
		
		return
	}
	
	// Source File
	fp, err := os.Create(path + "/" + j.Compile.SourceFileName)
	
	if err != nil {
		ch <- CreateInternalError("Failed to create source file." + err.Error())

		return
	}
	
	l, err := fp.Write([]byte(j.Code))
	
	if err != nil {
		ch <- CreateInternalError("Failed to write your code on your file. " + err.Error())

		return
	}
	
	if l != len(j.Code) {
		ch <- CreateInternalError("Failed to write your code on your file.")

		return
	}
	
	fp.Close()

	err = os.Chmod(path + "/" + j.Compile.SourceFileName, 0644)
	
	if err != nil {
		ch <- CreateInternalError("Failed to chmod the source file. " + err.Error())

		return
	}

	// Compile
	if j.Compile != nil {
		exe, err := NewExecutor(id, 512 * 1024 * 1024, j.Compile.Cmd, j.Compile.Image, []string{path + ":" + "/work"}, uid.Uid)
		
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
		
		if res.ExitCode != 0 { // Debug
			msg := res.Stdout + res.Stderr
			ch <- JudgeStatus{JR: CompileError, Msg: &msg}
			
			return
		}
	}
	
	exe, err := NewExecutor(id, j.Mem, j.Exec.Cmd, j.Exec.Image, []string{path + ":" + "/work:ro"}, uid.Uid)
	
	if err != nil {
		ch <- CreateInternalError("Failed to create a Docker container to judge." + err.Error())

		return
	}
	
	defer exe.Delete()
	
	tcCounter := 0
	
	totalResult := 0
	maxInt := func(a int, b int) int {
		if a > b {
			return a
		}else {
			return b
		}
	}
	maxInt64 := func(a int64, b int64) int64 {
		if a > b {
			return a
		}else {
			return b
		}
	}
	
	var maxTime, maxMem int64
	for tc, res := <-tests; res; tc, res = <-tests {
		tcCounter++
		
		msg := strconv.FormatInt(int64(tcCounter), 10) + "/" + strconv.FormatInt(int64(j.TCCount), 10)
		ch <- JudgeStatus{JR: Judging, Msg: &msg}
		
		r := Accepted
		res := exe.Run(j.Time, tc.In)
		
		if res.Status != ExecFinished {
			switch res.Status {
			case ExecError:
				msg := "Failed to execute your code." + res.Stderr
				ch <- JudgeStatus{Case: &tc.Name, JR: InternalError, Msg: &msg}
				r = InternalError
				maxMem = -1
				maxTime = -1
			case ExecMemoryLimitExceeded:
				ch <- JudgeStatus{Case: &tc.Name, JR: MemoryLimitExceeded}
				r = MemoryLimitExceeded
				maxMem = -1
				maxTime = -1
			case ExecTimeLimitExceeded:
				ch <- JudgeStatus{Case: &tc.Name, JR: TimeLimitExceeded}
				r = InternalError
				maxMem = -1
				maxTime = -1
			}
		}else {
			if res.ExitCode != 0 {
				ch <- JudgeStatus{Case: &tc.Name, JR: RuntimeError}
				r = RuntimeError
				maxMem = -1
				maxTime = -1
			}else {
				if res.Stdout == tc.Out {
					ch <- JudgeStatus{Case: &tc.Name, JR: Accepted, Mem: res.Mem, Time: res.Time}
					r = Accepted
				}else {
					ch <- JudgeStatus{Case: &tc.Name, JR: WrongAnswer, Mem: res.Mem, Time: res.Time}
					r = WrongAnswer
				}
				if maxMem != -1 {
					maxMem = maxInt64(maxMem, res.Mem)
				}
				if maxTime != -1 {
					maxTime = maxInt64(maxTime, res.Time)
				}
			}
		}
		
		totalResult = maxInt(totalResult, int(r))
	}
	
	ch <- JudgeStatus{JR: JudgeResult(totalResult), Time: maxTime, Mem: maxMem}
}
