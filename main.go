package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/docker/engine-api/client"
)

// SettingsTemplate is a template of a setting json
const SettingsTemplate = `{
    "name": "test-server",
    "parallelism": 2,
    "cpu_usage": 100,
}`

// SettingsInterface is a interface of setting file
// Generated at https://mholt.github.io/json-to-go/
type SettingsInterface struct {
	Name        string `json:"name"`
	Parallelism int    `json:"parallelism"`
	CPUUsage    int    `json:"cpu_usage"`
}

var wdir string

func printe(err string) {
	os.Stderr.Write([]byte(err + "\n"))
}

func main() {
	var settingData SettingsInterface

	help := flag.Bool("help", false, "Display all options")
	wdir := flag.String("wdir", "/tmp/pj", "A directory to execute programs")
	server := flag.String("server", "ws://192.168.2.1:8080/", "popcon server address")
	settings := flag.String("settings", "./pj.json", "Settings of popcon-judge")

	flag.Parse()

	if help != nil && *help {
		flag.PrintDefaults()

		return
	}

	err := os.MkdirAll(*wdir, 0664)

	if err != nil {
		printe(err.Error())

		os.Exit(1)

		return
	}

	if _, err = os.Stat(*settings); err != nil {
		printe(err.Error())

		if fp, err := os.OpenFile(*settings, os.O_RDWR|os.O_CREATE, 0664); err != nil {
			printe("failed to create a setting file at '" + *settings + "'")
		} else {
			printe("created a setting file at '" + *settings + "'")

			fp.Write([]byte(SettingsTemplate))

			fp.Close()
		}

		os.Exit(1)

		return
	} else {
		fp, err := os.OpenFile(*settings, os.O_RDONLY, 0664)

		if err != nil {
			printe(err.Error())

			os.Exit(1)

			return
		}

		dec := json.NewDecoder(fp)

		err = dec.Decode(&settingData)

		if err != nil {
			printe("Failed to decode a json: " + err.Error())

			os.Exit(1)

			return
		}
	}

	headers := map[string]string{"User-Agent": "popcon-judge"}

	cli, err = client.NewClient("unix:///var/run/docker.sock", "v1.22", nil, headers)

	if err != nil {
		panic(err)
	}

	exe, err := NewExecutor("Hello", 100*1024*1024, []string{"/host_tmp/a.out"}, "ubuntu:16.04", []string{"/tmp:/host_tmp:ro"}, "")

	if err != nil {
		fmt.Println(err)

		return
	}

	res := exe.Run(1000, "Hello")

	fmt.Println(res.ExitCode, res.Mem, res.Time, res.Status, res.Stdout, res.Stderr)

	err = exe.Delete()

	if err != nil {
		fmt.Println(err)
	}

	fmt.Println(*wdir, *server)
}
