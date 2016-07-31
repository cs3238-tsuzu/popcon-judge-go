package transfer

import (
	"github.com/gorilla/websocket"
	"log"
	"os"
	"time"
	"errors"
)

func NewTransfer(ep, authID string, paral int) (*Transfer, error) {
    conn, resp, err := websocket.DefaultDialer.Dial(ep, map[string][]string{"Authentication": []string{authID}})
    
    if err != nil {
        return nil, err
    }
    
    if resp.StatusCode != 101 {
        return nil, errors.New(resp.Status)
    }
    
    return &Transfer{Conn: conn, Log: *log.New(os.Stdout, "websock: ", log.LstdFlags), Parallelism: paral}, nil
}

type TransferedResponse struct {
    Resp JudgeResponse
    NewJudge int
}

type Transfer struct {
    Conn *websocket.Conn
    Log log.Logger
    Parallelism int

    RequestChan chan JudgeRequest
    ResponseChan chan JudgeResponse
}

func (tr *Transfer) Run() bool { // Start goroutines
    if tr.Conn != nil {
        tr.Conn.WriteJSON(TransferedResponse{JudgeResponse{Sid: -1}, tr.Parallelism})

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
        
        t := TransferedResponse{r, 0}

        if r.Case == -1 {
            t.NewJudge = 1
        }

        err := tr.Conn.WriteJSON(t)

        if err != nil {
            tr.Log.Println(err.Error())

            time.Sleep(1)
        }
    }
}

func (tr *Transfer) reader() { // called from only Run()
    for {
        jr := JudgeRequest{}
        
        err := tr.Conn.ReadJSON(&jr)
        
        if err != nil {
            tr.Log.Println(err.Error())
            
            time.Sleep(time.Second * 1)
        }

        tr.RequestChan <- jr
    }
}

// フロントエンド(github.com/cs3238-tsuzu/popcon)と共通
type JudgeType int

const (
    JudgePerfectMatch JudgeType = 0
    JudgeRunningCode JudgeType = 1
)

type SubmissionStatus int64

const (
	InQueue             SubmissionStatus = 0 // Not used
	Judging             SubmissionStatus = 1
	Accepted            SubmissionStatus = 2
	WrongAnswer         SubmissionStatus = 3
	TimeLimitExceeded   SubmissionStatus = 4
	MemoryLimitExceeded SubmissionStatus = 5
	RuntimeError        SubmissionStatus = 6
	CompileError        SubmissionStatus = 7
	InternalError       SubmissionStatus = 8
)

type TestCase struct {
    Name string
    Input string
    Output string
}

type JudgeRequest struct {
    Sid int64 // Submission ID
    Code string
    Lang int64
    Type JudgeType
    Checker string
    CheckerLang int64
    Cases map[string]TestCase
    Time int64
    Mem int64
}

type JudgeResponse struct {
    Sid int64 //SubmissionID
    Status SubmissionStatus 
    Msg string
    Time int64
    Mem int64
    Case int
    CaseName string
}
