package main

import (
	"bufio"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"os"
	"os/signal"
	"strings"
	"sync/atomic"
	"syscall"
)

type ServerState int
type HandleClientState int

const (
	Initialization ServerState = iota
	ValidateArgs
	ParseIP
	MakeStorageDirectory
	SetListening
	Listening
	Termination
	FatalError
)

const (
	ReadNumFiles HandleClientState = iota
	ReadFileName
	ReadFileContent
	WriteFile
	ReceiveNextFile
	HandleError
	Exit
)

const (
	trans = "tcp"
	bufferSize = 1024 * 1024 // 1MB
	arguments = 3
)

type ServerFSM struct {
	err error
	currentState ServerState
	shouldRun 	 int32
	ip           string
	port         string
	storageDir   string
	listener     net.Listener
	sigChan      chan os.Signal
}

type HandleClientFSM struct {
	err error
	currentState HandleClientState
	numFiles int
	currentFile int
	fileName string
	storageDir string
	fileContent []byte
	reader *bufio.Reader
	con net.Conn
}

func NewServerFSM() *ServerFSM {
	return &ServerFSM  {
		currentState: Initialization,
		sigChan: make(chan os.Signal, 1),
		shouldRun: 1,
	}
}

func (fsm *ServerFSM) InitializeState() ServerState {
	signal.Notify(fsm.sigChan, syscall.SIGINT)
	go fsm.handleSignal()
	return ValidateArgs
}


func (fsm *ServerFSM) ValidateArgsState() ServerState {
	args := os.Args[1:]
	if len(args) != arguments {
		fsm.err =  errors.New("invalid number of arguments, <ip> <port> <storage Directory>")
		return FatalError
	}

	fsm.ip = args[0]
	fsm.port = args[1]
	fsm.storageDir = args[2]
	return ParseIP
}


func(fsm *ServerFSM) ParseIPState() ServerState {
	if strings.Contains(fsm.ip, ":") {
		fsm.ip = "[" + fsm.ip + "]"
	}
	return MakeStorageDirectory
}

func (fsm *ServerFSM) MakeStorageDirectoryState() ServerState {
	if _, err := os.Stat(fsm.storageDir); os.IsNotExist(err)  {
		err = os.Mkdir(fsm.storageDir, 0755)
		if err != nil {
			fsm.err = err
			return FatalError
		}
	}
	return SetListening
}

func (fsm *ServerFSM) SetListeningState() ServerState {
	fsm.listener, fsm.err = net.Listen(trans, fsm.ip + ":" + fsm.port)
	if fsm.err != nil {
		return FatalError
	}
	fmt.Println("Server Listening on " + fsm.ip + ":" + fsm.port)
	return Listening
}

func (fsm *ServerFSM) handleSignal() {
	<- fsm.sigChan
	if fsm.listener != nil {
		fsm.listener.Close()
	}
	atomic.StoreInt32(&fsm.shouldRun, 0)
	fsm.currentState = Termination
}


func (fsm *ServerFSM) ListeningState() ServerState {
	con, err := fsm.listener.Accept()
	if err != nil {
		if opErr, ok := err.(*net.OpError); ok && (opErr.Op == "accept" || opErr.Op == "close") {
			fmt.Println("Server closed connection")
			return Termination
		}
		return Listening
	}

	if atomic.LoadInt32(&fsm.shouldRun) == 0 {
		return Termination
	}

	go func(){
		handleClientFSM := NewHandleClientFSM(con, fsm.storageDir)
		handleClientFSM.Run()

	}()
	return Listening
}


func (fsm *ServerFSM)TerminationState() {
	if fsm.listener != nil {
		fsm.listener.Close()
	}
	fmt.Println("\nServer Exiting...")


}

func (fsm *ServerFSM) FatalErrorState() ServerState {
	fmt.Println("Fatal Error:", fsm.err)
	return Termination
}


func (fsm *ServerFSM) Run() {
	for {
		switch fsm.currentState {
		case Initialization:
			fsm.currentState = fsm.InitializeState()
		case ValidateArgs:
			fsm.currentState = fsm.ValidateArgsState()
		case ParseIP:
			fsm.currentState = fsm.ParseIPState()
		case MakeStorageDirectory:
			fsm.currentState = fsm.MakeStorageDirectoryState()
		case SetListening:
			fsm.currentState = fsm.SetListeningState()
		case Listening:
			fsm.currentState = fsm.ListeningState()
		case FatalError:
			fsm.currentState = fsm.FatalErrorState()
		case Termination:
			fsm.TerminationState()
			return
		}

	}
}


func NewHandleClientFSM(con net.Conn, storageDir string) *HandleClientFSM {
	return &HandleClientFSM {
		currentState: ReadNumFiles,
		con: con,
		storageDir: storageDir,
		reader: bufio.NewReader(con),
		currentFile: 0,
	}

}

func (fsm *HandleClientFSM) ReadNumFilesState() HandleClientState {
	fsm.numFiles, fsm.err = receiveInt(fsm.reader)
	if fsm.err != nil {
		return HandleError
	}
	return ReadFileName
}

func (fsm *HandleClientFSM) ReadFileNameState() HandleClientState {
	fileName, err := receiveBytes(fsm.reader)
	if err != nil {
		fsm.err = err
		return HandleError
	}
	fsm.fileName = string(fileName)
	return ReadFileContent
}

func (fsm *HandleClientFSM) ReadFileContentState() HandleClientState {
	fsm.fileContent,fsm.err = receiveBytes(fsm.reader)
	if fsm.err != nil {
		return HandleError
	}
	return WriteFile
}

func (fsm *HandleClientFSM) WriteFileState() HandleClientState {
	file, err := os.Create(fsm.storageDir + "/" +  fsm.fileName)
	if err != nil {
		fsm.err = err
		return HandleError
	}
	defer file.Close()
	_, err = file.Write(fsm.fileContent)
	if err != nil {
		fsm.err = err
		return HandleError

	}
	fmt.Println("created file " + fsm.fileName + " in " + fsm.storageDir)
	fsm.currentFile++
	return ReceiveNextFile
}

func (fsm *HandleClientFSM) ReceiveNextFileState() HandleClientState {
	if fsm.currentFile == fsm.numFiles {
		return Exit
	}
	return ReadFileName

}

func (fsm *HandleClientFSM) HandleErrorState() HandleClientState {
	if fsm.err.Error() == "EOF" {
		fmt.Println("Error: Client closed connection")
	}
	fmt.Println("Error:", fsm.err)
	return Exit
}

func (fsm *HandleClientFSM) Run() {
	for fsm.currentState != Exit {
		switch fsm.currentState {
		case ReadNumFiles:
			fsm.currentState = fsm.ReadNumFilesState()
		case ReadFileName:
			fsm.currentState = fsm.ReadFileNameState()
		case ReadFileContent:
			fsm.currentState = fsm.ReadFileContentState()
		case WriteFile:
			fsm.currentState = fsm.WriteFileState()
		case ReceiveNextFile:
			fsm.currentState = fsm.ReceiveNextFileState()
		case HandleError:
			fsm.currentState = fsm.HandleErrorState()
		case Exit:
			fsm.con.Close()
			return
		}
	}
}
func receiveBytes(reader *bufio.Reader) ([]byte, error) {
	size, err := receiveInt(reader)
	if err != nil {
		return nil, err
	}
	data := make([]byte, size)
	received := 0

	for received < size {
		remaining := size - received
		readSize := bufferSize
		if remaining < readSize {
			readSize = remaining
		}

		n, err := reader.Read(data[received : received+readSize])
		if err != nil {
			return nil, err
		}

		received += n
	}

	return data, nil
}


func receiveInt(reader *bufio.Reader) (int, error) {
	receivedByte := make([]byte, 4)
	_, err := reader.Read(receivedByte)
	if err != nil {
		return -1, err
	}
	receiveInt := binary.BigEndian.Uint32(receivedByte)

	return int(receiveInt), nil
}

func main() {
	fsm := NewServerFSM()
	fsm.Run()
}
