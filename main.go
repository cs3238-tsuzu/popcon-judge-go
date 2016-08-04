package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"strconv"

	"github.com/cs3238-tsuzu/popcon-judge-go/Transfer"
	"github.com/docker/engine-api/client"
)

// SettingsTemplate is a template of a setting json
const SettingsTemplate = `{
    "name": "test-server",
    "parallelism": 2,
    "cpu_usage": 100,
	"auth": "****",
	"languages": {}
}`

type Language struct {
	SourceFileName string
	Compile        bool
	CompileCmd     []string
	CompileImage   string
	ExecCmd        []string
	ExecImage      string
}

// SettingsInterface is a interface of setting file
// Generated at https://mholt.github.io/json-to-go/
type SettingsInterface struct {
	Name        string              `json:"name"`
	Parallelism int                 `json:"parallelism"`
	CPUUsage    int                 `json:"cpu_usage"`
	Auth        string              `json:"auth"`
	Languages   map[string]Language // string(lid int64)
}

func CreateStringPointer(str string) *string {
	return &str
}

func main() {
	help := flag.Bool("help", false, "Display all options")
	wdir := flag.String("wdir", "/tmp/pj", "A directory to execute programs")
	server := flag.String("server", "ws://192.168.2.1:8080/", "popcon server address")
	settings := flag.String("settings", "./pj.json", "Settings of popcon-judge")
	genlang := flag.Bool("genlang", false, "Generate language setting")

	flag.Parse()

	if *genlang {
		b, _ := json.Marshal(&Language{})
		fmt.Println(string(b))

		return
	}

	if help != nil && *help {
		flag.PrintDefaults()

		return
	}

	err := os.MkdirAll(*wdir, 0664)

	if err != nil {
		log.Println(err.Error())

		os.Exit(1)

		return
	}

	var languages map[int64]Language

	if _, err = os.Stat(*settings); err != nil {
		log.Println(err.Error())

		if fp, err := os.OpenFile(*settings, os.O_RDWR|os.O_CREATE, 0664); err != nil {
			log.Println("failed to create a setting file at '" + *settings + "'")
		} else {
			log.Println("created a setting file at '" + *settings + "'")

			fp.Write([]byte(SettingsTemplate))

			fp.Close()
		}

		os.Exit(1)

		return
	} else {
		fp, err := os.OpenFile(*settings, os.O_RDONLY, 0664)

		if err != nil {
			log.Println(err.Error())

			os.Exit(1)

			return
		}

		dec := json.NewDecoder(fp)

		err = dec.Decode(&settingData)

		if err != nil {
			log.Println("Failed to decode a json: " + err.Error())

			os.Exit(1)

			return
		}

		languages = make(map[int64]Language)

		for k, v := range settingData.Languages {
			lid, err := strconv.ParseInt(k, 10, 64)

			if err != nil {
				panic(err)
			}

			languages[lid] = v
		}
	}

	// Copy instances to global ones
	workingDirectory = *wdir

	headers := map[string]string{"User-Agent": "popcon-judge"}

	cli, err = client.NewClient("unix:///var/run/docker.sock", "v1.22", nil, headers)

	if err != nil {
		panic(err)
	}

	trans, err := transfer.NewTransfer(*server, settingData.Auth, settingData.Parallelism)

	if err != nil {
		panic(err)
	}

	trans.RequestChan = make(chan transfer.JudgeRequest, 10)
	trans.ResponseChan = make(chan transfer.JudgeResponse, 10)

	suc := trans.Run()

	if !suc {
		panic(err)
	}
	//goto jump
	for {
		req, suc := <-trans.RequestChan

		if !suc {
			return
		}

		go func() {
			j := Judge{}

			lang, has := languages[req.Lang]

			if !has {
				trans.ResponseChan <- transfer.JudgeResponse{
					Sid:    req.Sid,
					Status: transfer.InternalError,
					Msg:    "Unknown Language",
					Case:   -1,
				}

				return
			}

			j.Code = req.Code
			j.Time = req.Time * 1000      // sec -> ms
			j.Mem = req.Mem * 1000 * 1000 // MB ->bytes

			if lang.Compile {
				j.Compile = &ExecRequest{
					Image:          lang.CompileImage,
					Cmd:            lang.CompileCmd,
					SourceFileName: lang.SourceFileName,
				}
			}

			j.Exec = ExecRequest{
				Image:          lang.ExecImage,
				Cmd:            lang.ExecCmd,
				SourceFileName: "",
			}

			casesChan := make(chan TCType, len(req.Cases))
			statusChan := make(chan JudgeStatus, 10)

			var check Judge
			var checkerCasesChan chan TCType
			var checkerStatusChan chan JudgeStatus
			var checkerPassChan chan transfer.JudgeResponse

			if req.Type == transfer.JudgeRunningCode {
				check.Time = 3 * 1000
				check.Code = req.Checker
				check.Mem = 256 * 1000 * 1000

				lang, has := languages[req.CheckerLang]

				if !has {
					trans.ResponseChan <- transfer.JudgeResponse{
						Sid:    req.Sid,
						Status: transfer.InternalError,
						Msg:    "Unknown Language for Checker Program",
						Case:   -1,
					}

					return
				}

				if lang.Compile {
					check.Compile = &ExecRequest{
						Image:          lang.CompileImage,
						Cmd:            lang.CompileCmd,
						SourceFileName: lang.SourceFileName,
					}
				}

				check.Exec = ExecRequest{
					Image:          lang.ExecImage,
					Cmd:            lang.ExecCmd,
					SourceFileName: "",
				}

				checkerCasesChan = make(chan TCType, len(req.Cases))
				checkerStatusChan = make(chan JudgeStatus, 10)
				checkerPassChan = make(chan transfer.JudgeResponse, 10)

				go check.Run(checkerStatusChan, checkerCasesChan, make(chan bool))
			}

			stopJudgeChan := make(chan bool, 1)
			go j.Run(statusChan, casesChan, stopJudgeChan)

			go func() {
				for i := 0; i < len(req.Cases); i++ {
					c := req.Cases[strconv.FormatInt(int64(i), 10)]

					casesChan <- TCType{ID: i, In: c.Input, Out: nil}
				}
				close(casesChan)
			}()

			respArr := make([]transfer.JudgeResponse, len(req.Cases)+1)

			respArr[len(respArr)-1] = transfer.JudgeResponse{
				Sid:    req.Sid,
				Status: transfer.InternalError,
				Case:   -1,
			}

			go func() {
				totalStatus := transfer.Accepted

				defer func() {
					if req.Type == transfer.JudgeRunningCode {
						close(checkerCasesChan)
					}
				}()

				for {
					stat, has := <-statusChan

					if !has {
						return
					}
					
					if stat.Case == -1 {
						if stat.JR == MemoryLimitExceeded {
							totalStatus = transfer.MemoryLimitExceeded
						} else if stat.JR == TimeLimitExceeded {
							totalStatus = transfer.TimeLimitExceeded
						} else if stat.JR == RuntimeError {
							totalStatus = transfer.RuntimeError
						} else if stat.JR == InternalError {
							totalStatus = transfer.InternalError
						} else if stat.JR >= 6 && stat.JR <= 8 {
							totalStatus = transfer.CompileError

							if stat.JR == CompileTimeLimitExceeded {
								stat.Stderr = "Compile Time Limit Exceeded"
							} else if stat.JR == CompileMemoryLimitExceeded {
								stat.Stderr = "Compile Memory Limit Exceeded"
							}
						}

						res := transfer.JudgeResponse{
							Sid:    req.Sid,
							Status: totalStatus,
							Case:   -1,
							Msg:    stat.Stderr,
							Time:   stat.Time,
							Mem:    stat.Mem / 1000,
						}

						if req.Type == transfer.JudgePerfectMatch {
							trans.ResponseChan <- res
						} else {
							respArr[len(respArr)-1] = res
						}

						return
					} else {
						status := transfer.Accepted
						if stat.JR == Judging {
							jr := transfer.JudgeResponse{
									Sid:    req.Sid,
									Status: transfer.Judging,
									Case:   stat.Case,
									Msg:    fmt.Sprint(stat.Case, "/", len(req.Cases)),
								}
							if req.Type == transfer.JudgeRunningCode {
								checkerPassChan <- jr
							} else {
								trans.ResponseChan <- jr
							}
							continue
						}

						if stat.JR == MemoryLimitExceeded {
							status = transfer.MemoryLimitExceeded
						} else if stat.JR == TimeLimitExceeded {
							status = transfer.TimeLimitExceeded
						} else if stat.JR == RuntimeError {
							status = transfer.RuntimeError
						} else if stat.JR == InternalError {
							status = transfer.InternalError
						}

						res := transfer.JudgeResponse{
							Sid:      req.Sid,
							Status:   status,
							Case:     stat.Case,
							CaseName: req.Cases[strconv.FormatInt(int64(stat.Case), 10)].Name,
							Time:     stat.Time,
							Mem:      stat.Mem / 1000,
						}

						if status != transfer.Accepted {
							trans.ResponseChan <- res
						} else if req.Type == transfer.JudgeRunningCode {
							c := req.Cases[strconv.FormatInt(int64(stat.Case), 10)]

							respArr[stat.Case] = res
							checkerCasesChan <- TCType{ID: stat.Case, In: c.Input, Out: &stat.Stdout}
						} else {

							if stat.Stdout != req.Cases[strconv.FormatInt(int64(stat.Case), 10)].Output {
								res.Status = transfer.WrongAnswer
								totalStatus = transfer.WrongAnswer
							}
							trans.ResponseChan <- res
						}
					}
				}
			}()

			if req.Type == transfer.JudgeRunningCode {
				go func() {
					defer close(stopJudgeChan)

					setupFinished := false
					for {
						select {
						case stat, has := <-checkerStatusChan:

							if !has {
								return
							}

							if stat.Case == -1 {
								resp := respArr[len(respArr)-1]

								if stat.JR == Finished {
									if setupFinished {
										resp.Status = transfer.Accepted
									}
								} else if stat.JR == RuntimeError {
									resp.Status = transfer.WrongAnswer
								} else {
									resp.Msg = "Checker Program: " + JudgeResultCodeToStr[stat.JR]
									resp.Status = transfer.InternalError

									if stat.JR == CompileError {
										resp.Msg += "\n" + stat.Stderr
									}
								}

								trans.ResponseChan <- resp

								return
							} else {
								setupFinished = true
								resp := respArr[stat.Case]

								if stat.JR == Judging {
									continue
								} else if stat.JR == Finished {
									resp.Status = transfer.Accepted
								} else if stat.JR == RuntimeError {
									resp.Status = transfer.WrongAnswer
								} else {
									resp.Msg = "Checker Program: " + JudgeResultCodeToStr[stat.JR]
									resp.Status = transfer.InternalError
								}

								trans.ResponseChan <- resp
							}
						case jr := <-checkerPassChan:
							trans.ResponseChan <- jr

							if jr.Case == -1 {
								return
							}
						}
					}
				}()
			}
		}()
	}
	/*
		exe, err := NewExecutor("Hello", 100*1024*1024, []string{"/host_tmp/a.out"}, "ubuntu:16.04", []string{"/tmp:/host_tmp:ro"}, "")

		if err != nil {
			fmt.Println(err)

			return
		}

		res := exe.Run(1000, "Hello")
	*/ /*
		//jump:
		j := Judge{}

		j.Code = `
				#include <iostream>

				int main() {
					long long ll = 0;
					while(1) new int[10];
					for(int i = 0; i < 100000000; ++i) {
						ll += i;
					}
					std::cout << "Hello, world" << std::endl;
				}
			`
		j.Compile = &ExecRequest{
			Cmd:            []string{"g++", "-std=c++14", "/work/main.cpp", "-o", "/work/a.out"},
			Image:          "ubuntu-popcon",
			SourceFileName: "main.cpp",
		}
		j.Exec = ExecRequest{
			Cmd:            []string{"/work/a.out"},
			Image:          "ubuntu-popcon",
			SourceFileName: "",
		}
		j.Mem = 100 * 1024 * 1024
		j.Time = 2000

		js := make(chan JudgeStatus, 10)
		tc := make(chan TCType, 10)

		go j.Run(js, tc)

		tc <- TCType{In: "", ID: 0}
		close(tc)

		for c, res := <-js; res; c, res = <-js {
			fmt.Println(c)
		}*/

	//	fmt.Println(res.ExitCode, res.Mem, res.Time, res.Status, res.Stdout, res.Stderr)

	//	err = exe.Delete()
	/*	err =
		if err != nil {
			fmt.Println(err)
		}

	fmt.Println(*wdir, *server)*/
}
