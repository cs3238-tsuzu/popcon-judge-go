package main

import "os"
import "math/rand"
//import "os/exec"
import "strconv"
import "github.com/seehuhn/mt19937"
import "time"
//import "os/user"

type TCType struct {
	Name string
	In   string
}

type ExecRequest struct {
	Image          string
	Cmd            []string
	SourceFileName string
}

type Judge struct {
	Code    string
	Compile *ExecRequest
	Exec    ExecRequest
	Time    int64
	Mem     int64
}

type JudgeResult int

const (
    Finished                   JudgeResult = 0
	Accepted                   JudgeResult = 1
	WrongAnswer                JudgeResult = 2
	MemoryLimitExceeded        JudgeResult = 3
	TimeLimitExceeded          JudgeResult = 4
	RuntimeError               JudgeResult = 5
	InternalError              JudgeResult = 6
	Judging                    JudgeResult = 7
	CompileError               JudgeResult = 8
	CompileTimeLimitExceeded   JudgeResult = 9
	CompileMemoryLimitExceeded JudgeResult = 10
)

var JudgeResultToStr = [...]string{"Finished", "Accepted", "WrongAnswer", "MemoryLimitExceeded", "TimeLimitExceeded", "RuntimeError", "InternalError", "Judging", "CompileError", "CompileTimeLimitExceeded", "CompileMemoryLimitExceeded", "Finished"}

type JudgeStatus struct {
	Case *string `json:"case"`
	JR   JudgeResult `json:"jr"`
	Mem  int64	`json:"mem"`
	Time int64	`json:"time"`
    Stdout *string `json:"stdout"`
    Stderr *string `json:"stderr"` // error and messageMsg
}

func CreateInternalError(msg string) JudgeStatus {
	return JudgeStatus{JR: InternalError, Stderr: &msg}
}

const BASE_RAND_STRING = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"

func RandomName() string {
	rng := rand.New(mt19937.New())
	rng.Seed(time.Now().UnixNano())

	res := make([]byte, 0, 16)
	for i := 0; i < 16; i++ {
		res = append(res, BASE_RAND_STRING[rng.Intn(len(BASE_RAND_STRING))])
	}

	return string(res)
}

func (j *Judge) Run(ch chan<- JudgeStatus, tests <-chan TCType) {
	// Close a channel to send results of judging
	defer close(ch)

	// Identity
	id := RandomName()

    /*
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
    */
	// Working Directory
	path := workingDirectory + "/" + id

	err := os.Mkdir(path, 0777)

	if err != nil {
		ch <- CreateInternalError("Failed to create a directory. " + err.Error())

		return
	}

	defer os.RemoveAll(path)

	//err = os.Chown(path, int(uidInt), int(gidInt))

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

	err = os.Chmod(path+"/"+j.Compile.SourceFileName, 0644)

	if err != nil {
		ch <- CreateInternalError("Failed to chmod the source file. " + err.Error())

		return
	}

	// Compile
	if j.Compile != nil {
		exe, err := NewExecutor(id, 512*1024*1024, 10000, j.Compile.Cmd, j.Compile.Image, []string{path + ":" + "/work"})

		if err != nil {
			ch <- CreateInternalError("Failed to create a Docker container to compile your code." + err.Error())

			return
		}

		res := exe.Run("")

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
			ch <- JudgeStatus{JR: CompileError, Stderr: &msg}

			return
		}
	}

	exe, err := NewExecutor(id, j.Mem, j.Time, j.Exec.Cmd, j.Exec.Image, []string{path + ":" + "/work:ro"})

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
		} else {
			return b
		}
	}
	maxInt64 := func(a int64, b int64) int64 {
		if a > b {
			return a
		} else {
			return b
		}
	}

	var maxTime, maxMem int64

	for tc, res := <-tests; res; tc, res = <-tests {
		tcCounter++

		msg := strconv.FormatInt(int64(tcCounter), 10)
		ch <- JudgeStatus{JR: Judging, Stderr: &msg}

		r := Accepted
		res := exe.Run(tc.In)
		
		name := tc.Name
		if res.Status != ExecFinished {
			switch res.Status {
			case ExecError:
				msg := "Failed to execute your code. " + res.Stderr
				ch <- CreateInternalError(msg)
				r = InternalError
				maxMem = -1
				maxTime = -1
			case ExecMemoryLimitExceeded:
				ch <- JudgeStatus{Case: &name, JR: MemoryLimitExceeded}
				r = MemoryLimitExceeded
				maxMem = -1
				maxTime = -1
			case ExecTimeLimitExceeded:
				ch <- JudgeStatus{Case: &name, JR: TimeLimitExceeded}
				r = InternalError
				maxMem = -1
				maxTime = -1
			}
		} else {
			if res.ExitCode != 0 {
				ch <- JudgeStatus{Case: &name, JR: RuntimeError}
				r = RuntimeError
				maxMem = -1
				maxTime = -1
			} else {
				/*if res.Stdout == tc.Out {
					ch <- JudgeStatus{Case: &name, JR: Accepted, Mem: res.Mem, Time: res.Time, Stdout: &res.Stdout, Stderr: &res.Stderr}
					r = Accepted
				} else {
					ch <- JudgeStatus{Case: &name, JR: WrongAnswer, Mem: res.Mem, Time: res.Time, Stdout: &res.Stdout, Stderr: &res.Stderr}
					r = WrongAnswer
				}*/
                ch <- JudgeStatus{Case: &name, JR: Finished, Mem: res.Mem, Time: res.Time, Stdout: &res.Stdout, Stderr: &res.Stderr}
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