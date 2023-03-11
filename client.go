package main

import (
	"bytes"
	"crypto/aes"
	"encoding/json"
	"errors"
	"fmt"
	cn "github.com/ilinovalex86/connection"
	ex "github.com/ilinovalex86/explorer"
	"github.com/ilinovalex86/screenshot"
	si "github.com/ilinovalex86/sendinput"
	"image/jpeg"
	"io/ioutil"
	"log"
	"net"
	"os"
	"runtime"
	"sync"
	"time"
)

const configFile = "conf.txt"
const version = "0.0.15"
const key = "2112751343910015"
const logFileName = "log.txt"

var l = logData{fileName: logFileName}

type logData struct {
	m        sync.Mutex
	fileName string
	eol      string
}

func toLog(data string, flag bool) {
	data = "client " + time.Now().Format("02.01.2006 15:04:05") + " " + data
	l.m.Lock()
	file, err := os.OpenFile(l.fileName, os.O_APPEND|os.O_WRONLY, 0666)
	if err != nil {
		log.Fatal("open log file")
	}
	_, err = file.WriteString(data + l.eol)
	if err != nil {
		file.Close()
		log.Fatal("write data to log")
	}
	file.Close()
	l.m.Unlock()
	if flag {
		log.Fatal(data)
	}
}

var conf config

type config struct {
	UpdaterServer string
	TcpServer     string
	StreamServer  string
	VersionClient string
	ClientId      string
}

//Структура клиента
type clientData struct {
	Sep      string
	BasePath string
	conn     net.Conn
	Version  string
	System   string
}

type stream struct {
	sync.Mutex
	imgI int
	imgB []byte
	err  bool
	conn net.Conn
}

type event struct {
	Method string
	Event  string
	Key    string
	Code   string
	CorX   int
	CorY   int
	Ctrl   bool
	Shift  bool
}

var quality = 90

// Папка для скачивания файлов
var uploadDir = "files"

func dumpConf(conf config) {
	const funcNameLog = "dumpConf(): "
	data, err := json.MarshalIndent(&conf, "", "  ")
	if err != nil {
		toLog(funcNameLog+"json.MarshalIndent", true)
	}
	err = ioutil.WriteFile(configFile, data, 0644)
	if err != nil {
		toLog(funcNameLog+"ioutil.WriteFile(configFile, data, 0644)", true)
	}
}

func init() {
	const funcNameLog = "init(): "
	if !ex.ExistDir(uploadDir) {
		err := ex.MakeDir(uploadDir)
		if err != nil {
			toLog(funcNameLog+"ex.MakeDir(uploadDir)", true)
		}
	}
	if runtime.GOOS == "windows" {
		l.eol = "\r\n"
	}
	if runtime.GOOS == "linux" {
		l.eol = "\n"
	}
	if ex.ExistFile(configFile) {
		data, err := ex.ReadFileFull(configFile)
		if err != nil {
			toLog(funcNameLog+"read conf file", true)
		}
		err = json.Unmarshal(data, &conf)
		if err != nil {
			toLog(funcNameLog+"json.Unmarshal(data, &conf)", true)
		}
		if conf.VersionClient != version {
			conf.VersionClient = version
			dumpConf(conf)
		}
	} else {
		conf := config{
			UpdaterServer: "127.0.0.1:50000",
			TcpServer:     "127.0.0.1:50001",
			StreamServer:  "127.0.0.1:50002",
			VersionClient: "0.0.0",
			ClientId:      "----------------",
		}
		dumpConf(conf)
		toLog("Файл конфигурации не найден. Создан новый файл конфигурации.", true)
	}
}

//Определяет тип ОС, имя пользователя, домашнюю папку, id.
func newClient() *clientData {
	cl := &clientData{
		Sep:      ex.Sep,
		BasePath: ex.BasePath,
		Version:  conf.VersionClient,
		System:   ex.System,
	}
	return cl
}

//Обрабатывает подключение к серверу и передает данные о клиенте.
func (cl *clientData) connect() error {
	const funcNameLog = "cl.connect(): "
	if !cl.validOnServer(cl.conn) {
		toLog(funcNameLog+"Valid on Server", true)
	}
	err := cn.SendString(conf.ClientId, cl.conn)
	if err != nil {
		return errors.New(funcNameLog + "cn.SendString(conf.ClientId, cl.conn)")
	}
	cn.ReadSync(cl.conn)
	jsonData, err := json.Marshal(cl)
	if err != nil {
		return errors.New(funcNameLog + "json.Marshal(cl)")
	}
	err = cn.SendBytesWithDelim(jsonData, cl.conn)
	if err != nil {
		return errors.New(funcNameLog + "cn.SendBytesWithDelim(jsonData, cl.conn)")
	}
	q, err := cn.ReadQuery(cl.conn)
	if err != nil {
		return errors.New(funcNameLog + "cn.ReadQuery(cl.conn)")
	}
	switch q.Method {
	case "wrong version":
		toLog(funcNameLog+"wrong version", true)
	case "connect":
		return nil
	case "new id":
		err = cl.newId()
		if err != nil {
			return errors.New(funcNameLog + fmt.Sprint(err))
		}
	case "already exist":
		toLog(funcNameLog+"already exist", true)
	}
	return nil
}

//Проходит проверку на подключение к серверу
func (cl *clientData) validOnServer(conn net.Conn) bool {
	const funcNameLog = "cl.validOnServer(): "
	var code = make([]byte, 16)
	bc, err := aes.NewCipher([]byte(key))
	if err != nil {
		toLog(funcNameLog+"aes.NewCipher([]byte(key))", false)
		return false
	}
	err = cn.SendString(cl.Version, conn)
	if err != nil {
		toLog(funcNameLog+"cn.SendString(cl.Version, conn)", false)
		return false
	}
	data, err := cn.ReadBytesByLen(16, conn)
	if err != nil {
		toLog(funcNameLog+"cn.ReadBytesByLen(16, conn)", false)
		return false
	}
	bc.Decrypt(code, data)
	s := string(code)
	res := s[len(s)/2:] + s[:len(s)/2]
	bc.Encrypt(code, []byte(res))
	err = cn.SendBytes(code, conn)
	if err != nil {
		toLog(funcNameLog+"cn.SendBytes(code, conn)", false)
		return false
	}
	mes, err := cn.ReadString(conn)
	if err != nil {
		toLog(funcNameLog+"cn.ReadString(conn)", false)
		return false
	}
	if mes != "ok" {
		toLog(funcNameLog+"mes != \"ok\"", false)
		return false
	}
	return true
}

//Получает новый id от сервера и сохраняет его
func (cl *clientData) newId() error {
	const funcNameLog = "cl.newId()"
	cn.SendSync(cl.conn)
	var err error
	conf.ClientId, err = cn.ReadString(cl.conn)
	if err != nil {
		return errors.New(funcNameLog + "cn.ReadString(cl.conn)")
	}
	dumpConf(conf)
	fmt.Println("New Id: ", conf.ClientId)
	return nil
}

//Обрабатывает запрос на содержимое папки
func (cl *clientData) dir(path string) error {
	const funcNameLog = "cl.dir(): "
	if path == "" {
		path = cl.BasePath
	}
	if ex.ExistDir(path) {
		res, err := ex.Explorer(path)
		if err != nil {
			toLog(funcNameLog+"ex.Explorer("+path+")", false)
			err = cn.SendResponse(cn.Response{Err: err}, cl.conn)
			if err != nil {
				return errors.New(funcNameLog + "cn.SendResponse(cn.Response{Err: err}, cl.conn)")
			}
			return nil
		}
		res["nav"] = ex.NavFunc(path)
		data, err := json.Marshal(res)
		if err != nil {
			toLog(funcNameLog+"json.Marshal(res)", false)
			err = cn.SendResponse(cn.Response{Err: err}, cl.conn)
			if err != nil {
				return errors.New(funcNameLog + "cn.SendResponse(cn.Response{Err: err}, cl.conn)")
			}
			return nil
		}
		err = cn.SendResponse(cn.Response{DataLen: len(data)}, cl.conn)
		if err != nil {
			return errors.New(funcNameLog + "cn.SendResponse(cn.Response{DataLen: len(data)}, cl.conn)")
		}
		cn.ReadSync(cl.conn)
		err = cn.SendBytes(data, cl.conn)
		if err != nil {
			return errors.New(funcNameLog + "cn.SendBytes(data, cl.conn)")
		}
	} else {
		err := cn.SendResponse(cn.Response{Err: errors.New(path + " is not exist")}, cl.conn)
		if err != nil {
			return errors.New(funcNameLog + "cn.SendResponse(cn.Response{Err: errors.New(path + \" is not exist\")}, cl.conn)")
		}
	}
	return nil
}

//Обрабатывает запрос на отправку файла
func (cl *clientData) fileFromClient(path string) error {
	const funcNameLog = "cl.fileFromClient(): "
	if ex.ExistFile(path) {
		file, err := os.Stat(path)
		if err != nil {
			return errors.New(funcNameLog + "os.Stat(" + path + ")")
		}
		err = cn.SendResponse(cn.Response{DataLen: int(file.Size()), Response: file.Name()}, cl.conn)
		if err != nil {
			return errors.New(funcNameLog + "cn.SendResponse(cn.Response{DataLen: int(file.Size()), Response: file.Name()}, cl.conn)")
		}
		cn.ReadSync(cl.conn)
		err = cn.SendFile(path, cl.conn)
		if err != nil {
			return errors.New(funcNameLog + "cn.SendFile(path, cl.conn)")
		}
	} else {
		err := cn.SendResponse(cn.Response{Err: errors.New(path + " is not exist")}, cl.conn)
		if err != nil {
			return errors.New(funcNameLog + "cn.SendResponse(cn.Response{Err: errors.New(path + \" is not exist\")}, cl.conn)")
		}
	}
	return nil
}

func eventsRun(events []event) {
	m := si.MouseEvent{}
	k := si.KeyboardEvent{}
	for _, e := range events {
		switch e.Method {
		case "mouse":
			m = si.MouseEvent{CordX: e.CorX, CordY: e.CorY}
			switch e.Event {
			case "move":
				m.Move()
			case "left":
				if e.Shift {
					k.ShiftPress()
				}
				if e.Ctrl {
					k.CtrlPress()
				}
				time.Sleep(time.Millisecond)
				m.LClick()
				time.Sleep(time.Millisecond)
				if e.Shift {
					k.ShiftRelease()
				}
				if e.Ctrl {
					k.CtrlRelease()
				}
			case "right":
				m.RClick()
			case "dbclick":
				m.DoubleClick()
			case "drop":
				m.Drop()
			case "scrollUp":
				m.WheelUp()
			case "scrollDown":
				m.WheelDown()
			}
		case "keyboard":
			k = si.KeyboardEvent{Ctrl: e.Ctrl, Shift: e.Shift, JavaScriptCode: e.Code}
			k.Launching()
		}
		time.Sleep(time.Millisecond)
	}
}

func (s *stream) errTrue() {
	s.Lock()
	defer s.Unlock()
	s.err = true
	s.conn.Close()
}

func (s *stream) stream() {
	const funcNameLog = "s.stream(): "
	eventStatus := ""
	var err error
	var events []event
	var imgI int
	for {
		var imgB []byte
		if eventStatus == "yes" {
			events = nil
			jsonEvents, err := cn.ReadByteByDelim(s.conn)
			if err != nil {
				toLog(funcNameLog+"cn.ReadByteByDelim(s.conn)", false)
				break
			}
			err = json.Unmarshal(jsonEvents, &events)
			if err != nil {
				toLog(funcNameLog+"json.Unmarshal(jsonEvents, &events)", false)
				break
			}
			eventsRun(events)
		} else {
			cn.ReadSync(s.conn)
		}
		for {
			s.Lock()
			if s.err {
				s.Unlock()
				return
			}
			if imgI == s.imgI {
				s.Unlock()
				time.Sleep(time.Millisecond * 5)
				continue
			}
			imgI = s.imgI
			imgB = s.imgB
			s.Unlock()
			break
		}
		err = cn.SendResponse(cn.Response{DataLen: len(imgB)}, s.conn)
		if err != nil {
			toLog(funcNameLog+"cn.SendResponse(cn.Response{DataLen: len(imgB)}, s.conn)", false)
			break
		}
		eventStatus, err = cn.ReadString(s.conn)
		if err != nil {
			toLog(funcNameLog+"cn.ReadString(s.conn)", false)
			break
		}
		err = cn.SendBytes(imgB, s.conn)
		if err != nil {
			toLog(funcNameLog+"cn.SendBytes(imgB, s.conn)", false)
			break
		}
	}
	s.errTrue()
}

//Обрабатывает запрос на отправку файла
func (cl *clientData) stream(c chan error) {
	const funcNameLog = "cl.stream(): "
	streamConn, err := net.Dial("tcp", conf.StreamServer)
	if err != nil {
		c <- errors.New(funcNameLog + "streamServer not found")
		return
	}
	toLog("connect to conf.StreamServer: "+conf.StreamServer, false)
	if !cl.validOnServer(streamConn) {
		c <- errors.New(funcNameLog + "valid stream server")
		return
	}
	toLog("stream connected", false)
	err = cn.SendString(conf.ClientId, streamConn)
	if err != nil {
		c <- errors.New(funcNameLog + "cn.SendString(conf.ClientId, streamConn)")
	}
	cn.ReadSync(streamConn)
	jsonData, err := json.Marshal(cl)
	if err != nil {
		c <- errors.New(funcNameLog + "json.Marshal(cl)")
		return
	}
	err = cn.SendBytesWithDelim(jsonData, streamConn)
	if err != nil {
		c <- errors.New(funcNameLog + "cn.SendBytesWithDelim(jsonData, streamConn)")
		return
	}
	con, err := screenshot.Connect()
	if err != nil {
		c <- errors.New(funcNameLog + "screenshot.Connect()")
		return
	}
	defer screenshot.Close(con)
	ss := screenshot.ScreenSize(con)
	jsonSS, err := json.Marshal(ss)
	if err != nil {
		c <- errors.New(funcNameLog + "json.Marshal(ss)")
		return
	}
	cn.ReadSync(streamConn)
	err = cn.SendBytesWithDelim(jsonSS, streamConn)
	if err != nil {
		c <- errors.New(funcNameLog + "cn.SendBytesWithDelim(jsonSS, streamConn)")
		return
	}
	c <- nil
	s := &stream{conn: streamConn}
	go s.stream()
	for {
		img, err := screenshot.CaptureScreen(con)
		if err != nil {
			toLog(funcNameLog+"screenshot.CaptureScreen(con)", false)
			break
		}
		buf := new(bytes.Buffer)
		err = jpeg.Encode(buf, img, &jpeg.Options{Quality: quality})
		if err != nil {
			toLog(funcNameLog+"jpeg.Encode(buf, img, &jpeg.Options{Quality: quality})", false)
			break
		}
		imgB := buf.Bytes()
		if len(imgB)/1024 > 350 && quality > 85 {
			quality--
		}
		if len(imgB)/1024 < 300 && quality < 95 {
			quality++
		}
		s.Lock()
		if s.err {
			break
		}
		s.imgB = imgB
		s.imgI++
		s.Unlock()
	}
	s.errTrue()
	toLog("stream stop", false)
	fmt.Println("stream stop")
}

func (cl *clientData) fileToClient(q cn.Query) error {
	const funcNameLog = "cl.fileToClient(): "
	switch ex.System {
	case "linux":
		uploadDir += "/" + q.Query
	case "windows":
		uploadDir += "\\" + q.Query
	}
	cn.SendSync(cl.conn)
	err := cn.GetFile(uploadDir, q.DataLen, cl.conn)
	if err != nil {
		err = cn.SendResponse(cn.Response{Err: err}, cl.conn)
		if err != nil {
			return errors.New(funcNameLog + "cn.SendResponse(cn.Response{Err: err}, cl.conn)")
		}
		return errors.New(funcNameLog + "cn.GetFile(uploadDir, q.DataLen, cl.conn)")
	}
	err = cn.SendResponse(cn.Response{}, cl.conn)
	if err != nil {
		return errors.New(funcNameLog + "cn.SendResponse(cn.Response{}, cl.conn)")
	}
	return nil
}

//Принимает сообщения от сервера и обрабатывает их.
func worker(cl *clientData) error {
	const funcNameLog = "worker(): "
	for {
		q, err := cn.ReadQuery(cl.conn)
		if err != nil {
			return errors.New(funcNameLog + "server disconnected")
		}
		toLog(fmt.Sprintf("--> %#v", q), false)
		fmt.Printf("%#v %s \n", q, time.Now().Format("02.01.2006 15:04:05"))
		switch q.Method {
		case "testConnect":
			err = cn.SendResponse(cn.Response{}, cl.conn)
			if err != nil {
				return errors.New(funcNameLog + "cn.SendResponse(cn.Response{}, cl.conn)")
			}
		case "dir":
			err = cl.dir(q.Query)
			if err != nil {
				return errors.New(funcNameLog + fmt.Sprint(err))
			}
		case "fileFromClient":
			err = cl.fileFromClient(q.Query)
			if err != nil {
				return errors.New(funcNameLog + fmt.Sprint(err))
			}
		case "fileToClient":
			err = cl.fileToClient(q)
			if err != nil {
				return errors.New(funcNameLog + fmt.Sprint(err))
			}
		case "stream":
			channel := make(chan error)
			go cl.stream(channel)
			err = <-channel
			if err != nil {
				err2 := cn.SendResponse(cn.Response{Err: err}, cl.conn)
				if err2 != nil {
					return errors.New(funcNameLog + "cn.SendResponse(cn.Response{Err: err}, cl.conn)")
				}
				return errors.New(funcNameLog + fmt.Sprint(err))
			}
			err = cn.SendResponse(cn.Response{}, cl.conn)
			if err != nil {
				return errors.New(funcNameLog + "cn.SendResponse(cn.Response{}, cl.conn)")
			}
		default:
			return errors.New(funcNameLog + "something wrong")
		}
	}
}

//Подключается к серверу, выводит ошибки
func main() {
	const funcNameLog = "main(): "
	toLog("start client", false)
	cl := newClient()
	conn, err := net.Dial("tcp", conf.TcpServer)
	if err != nil {
		toLog(funcNameLog+"server not found", true)
	}
	toLog("connect to conf.TcpServer: "+conf.TcpServer, false)
	cl.conn = conn
	err = cl.connect()
	if err != nil {
		conn.Close()
		toLog(funcNameLog+fmt.Sprint(err), true)
	}
	toLog("client connected", false)
	fmt.Printf("Client connected %s \n", time.Now().Format("02.01.2006 15:04:05"))
	err = worker(cl)
	if err != nil {
		fmt.Println(err)
		conn.Close()
		toLog(funcNameLog+fmt.Sprint(err), true)
	}
}
