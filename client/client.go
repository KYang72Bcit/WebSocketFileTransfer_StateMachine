package main

import (
	"bufio"
	"encoding/binary"
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"strings"
)

const (
	trans = "tcp"
	bufferSize = 1024 * 1024
	arguments = 3
)

type ClientState int

type ClientFSM struct {
	err error
	currentState ClientState
	ip           string
	port         string
	fileNames    []string
	currentFile  int
	con          net.Conn
	writer 		 *bufio.Writer
	file 		 *os.File
}


const (
	Initialization ClientState = iota
	ValidateArgs
	ParseIP
	ConnetServer
	SendFileCount
	OpenFile
	SendFileName
	ReadAndSendFileData
	SendNextFile
	HandleFatalError
	HandleError
	Terminate

)


func NewClientFSM() *ClientFSM {
	return &ClientFSM {
		currentState: ValidateArgs,
	}
}


func (fsm *ClientFSM) ValidateArgsState() ClientState {
	args := os.Args[1:]
	if len(args) < arguments {
		fsm.err = errors.New("invalid number of arguments, <ip> <port> <filename1>...<filenameN>")
		return HandleFatalError
	}
	fsm.ip = args[0]
	fsm.port = args[1]
	fsm.fileNames = args[2:]
	return ParseIP
}

func (fsm *ClientFSM) ParseIPState() ClientState {
	if strings.Contains(fsm.ip, ":") {
		fsm.ip = "[" + fsm.ip + "]"
	}
	return ConnetServer
}

func (fsm *ClientFSM) ConnetServerState() ClientState {
	fsm.con, fsm.err = net.Dial(trans, fsm.ip + ":" + fsm.port)
	if fsm.err != nil {
		return HandleFatalError
	}
	fsm.writer = bufio.NewWriter(fsm.con)
	return SendFileCount
}

func (fsm *ClientFSM) SendFileCountState() ClientState {
	err := sendInt(fsm.writer, len(fsm.fileNames))
	if err != nil {
		fsm.err = err
		return HandleFatalError
	}
	fsm.currentFile = 0
	return OpenFile
}

func (fsm *ClientFSM) OpenFileState() ClientState {
	fsm.file, fsm.err = os.Open(fsm.fileNames[fsm.currentFile])
	if fsm.err != nil {
		return HandleError
	}
	return SendFileName
}

func (fsm *ClientFSM) SendFileNameState() ClientState {
	fileInfo, err := fsm.file.Stat()
	if err != nil {
		fsm.err = err
		fsm.file.Close()
		return HandleError
	}
	fname := []byte(fileInfo.Name())
	_, fsm.err = sendBytes(fsm.writer, fname)
	if fsm.err != nil {
		fsm.file.Close()
		return HandleFatalError }
	return ReadAndSendFileData

}

func (fsm *ClientFSM) ReadAndSendFileDataState() ClientState {
	fileData, err := os.ReadFile(fsm.fileNames[fsm.currentFile])

	if err != nil {
		fsm.err = err
		fsm.file.Close()
		return HandleError
	}
	_, fsm.err = sendBytes(fsm.writer, fileData)
	if fsm.err != nil {
		fsm.file.Close()
		return HandleFatalError
	}
	fsm.file.Close()
	println("Sent file " + fsm.fileNames[fsm.currentFile])
	fsm.currentFile++

	return SendNextFile
}

func (fsm *ClientFSM) SendNextFileState() ClientState {
	if fsm.currentFile >= len(fsm.fileNames) {
		return Terminate
	}
	return OpenFile
}

func (fsm *ClientFSM) HandleFatalErrorState() ClientState {
	log.Println("Fatal Error:", fsm.err)
	return Terminate
}

func (fsm *ClientFSM) HandleFileError() ClientState {
	fmt.Println("Error:", fsm.err)
	fsm.currentFile++ //need to check if this is correct
	return SendNextFile
}

func (fsm *ClientFSM) TerminateState() {
	if fsm.con != nil {
		fsm.con.Close()
	}
	fmt.Println("Client Exiting...")
}

func (fsm *ClientFSM) Run() {
	for {
		switch fsm.currentState {
		case ValidateArgs:
			fsm.currentState = fsm.ValidateArgsState()
		case ParseIP:
			fsm.currentState = fsm.ParseIPState()
		case ConnetServer:
			fsm.currentState = fsm.ConnetServerState()
		case SendFileCount:
			fsm.currentState = fsm.SendFileCountState()
		case OpenFile:
			fsm.currentState = fsm.OpenFileState()
		case SendFileName:
			fsm.currentState = fsm.SendFileNameState()
		case ReadAndSendFileData:
			fsm.currentState = fsm.ReadAndSendFileDataState()
		case SendNextFile:
			fsm.currentState = fsm.SendNextFileState()
		case HandleFatalError:
			fsm.currentState = fsm.HandleFatalErrorState()
		case HandleError:
			fsm.currentState = fsm.HandleFileError()
		case Terminate:
			fsm.TerminateState()
			return
		}
	}
}


// sendInt encodes the provided integer using big endian and sends it to the provided writer
// It returns an error if the writer cannot be written to
// error will be nil if there's no error
func sendInt(writer *bufio.Writer, num int) (error) {
	intSend := int32(num)
	sendBytes := make([]byte, 4)
	binary.BigEndian.PutUint32(sendBytes, uint32(intSend))
	_, err := writer.Write(sendBytes)
	if err != nil {
		return err
	}
	return writer.Flush()
}

// sendBytes sends the provided byte array to the provided writer
// It returns an int of the number of data it send, and error if the writer cannot be written to
// error will be nil if there's no error
func sendBytes(writer *bufio.Writer, data []byte) (int, error) {
	err := sendInt(writer, len(data))
	if err != nil {
		return -1, err
	}

	for start := 0; start < len(data); start += bufferSize {
		end := start + bufferSize
		if end > len(data) {
			end = len(data)
		}

		chunk := data[start:end]
		_, err := writer.Write(chunk)
		if err != nil {
			return -1, err
		}
		err = writer.Flush()
		if err != nil {
			return -1, err
		}
	}
	return len(data), nil
}



//validates the provided arguments
//returns the ip, port, filenames and error
func validateArgs(args []string) (ip string, port string, filenames []string, err error){
	if len(args) < arguments {
		return "", "", nil, errors.New("invalid number of arguments, <ip> <port> <filename1>...<filenameN>")
	}

	ip = args[0]
	port = args[1]
	filenames = args[2:]

	return ip, port, filenames, nil
}


func main(){

	clientFSM := NewClientFSM()
	clientFSM.Run()

}
