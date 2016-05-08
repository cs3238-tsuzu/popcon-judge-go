package main

import (
    "fmt"
    "flag"
    "os"
    "encoding/json"
)

// SettingsTemplate is a template of a setting json
const SettingsTemplate = `{
    "name": "test-server",
    "parallelism": 2,
    "cpu_usage": 100,
    "version": "0.01"
}`

// SettingsInterface is a interface of setting file
// Generated at https://mholt.github.io/json-to-go/
type SettingsInterface struct {
	Name string `json:"name"`
	Parallelism int `json:"parallelism"`
	CPUUsage int `json:"cpu_usage"`
	Version string `json:"version"`
}

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
        
        if fp, err := os.OpenFile(*settings, os.O_RDWR | os.O_CREATE, 0664); err != nil {
            printe("failed to create a setting file at '" + *settings + "'")
        }else {
            printe("created a setting file at '" + *settings + "'")

            fp.Write([]byte(SettingsTemplate))
            
            fp.Close()
        }
        
        os.Exit(1)
        
        return
    }else {
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
    
    fmt.Println(*wdir, *server)
}