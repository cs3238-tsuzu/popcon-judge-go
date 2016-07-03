package main

import (
	"github.com/gorilla/websocket"
	"net/http"
	"fmt"
	"log"
	"os"
	"time"
	"errors"
)

func NewTransfer(ep, authID string) (*Transfer, error) {
    conn, resp, err := websocket.DefaultDialer.Dial(ep, map[string][]string{"Authentication": []string{authID}})
    
    if err != nil {
        return nil, err
    }
    
    if resp.StatusCode != 200 {
        return nil, errors.New(fmt.Sprint(resp.StatusCode, " ", resp.Status))
    }
    
    return &Transfer{Conn: conn, Log: *log.New(os.Stdout, "websock: ", log.LstdFlags)}, nil
}

type Transfer struct {
    Conn *websocket.Conn
    Log log.Logger

    JudgeRequestChan chan JudgeRequest
    ResponseChan chan TransferResponse
}

func (tr *Transfer) Run() bool { // Start goroutines
    if tr.Conn != nil {
        go tr.writer()
        go tr.reader()
        
        return true
    }else {
        return false
    }
}

func (tr *Transfer) writer() { // called from only Run()
    for {
        r, ok := <-tr.ResponseChan
        
        if !ok {
            return
        }
        
        err := tr.Conn.WriteJSON(r)

        if err != nil {
            tr.Log.Println(err.Error())

            time.Sleep(1)
        }
    }
}

func (tr *Transfer) reader() { // called from only Run()
    for {
        re := TransferRequest{}
        
        err := tr.Conn.ReadJSON(&re)
        
        if err != nil {
            tr.Log.Println(err.Error())
            
            time.Sleep(time.Second * 1)
        }
    }
}

type RequestType int

const (
    RequestJudge RequestType = 0
)

type TransferRequest struct {
    ReqType RequestType `json:"request_type"`
    Data string `json: "data"`
}

type JudgeTypeType int

const (
    JudgeTypeSimple JudgeTypeType = 0
)

type JudgeRequest struct {
    CID string `json:"cid"` // ContestID
    SID string `json:"sid"` // Submission ID
    Code string `json:"code"`
    Lang string `json:"lang"`
    JudgeType string `json:"type"`
    Cases []string `json:"type"`
}

type ResponseType int

const (
    ResponseJudge ResponseType = 0
)

type TransferResponse struct {
    ResType ResponseType `json:"response_type"`
    Data string `json:"data"`
}

type JudgeResponse struct {
    CID string `json:"cid"` //ContestID
    SID string `json:"sid"` //SubmissionID
    Status int `json:"status"`
    Msg *string `json:"msg"`
    Time int64 `json:"time"`
    Mem int64 `json:"mem"`
}