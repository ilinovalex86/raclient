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
	"sync"
	"time"
)

const configFile = "conf.txt"
const version = "0.0.12"
const key = "2112751343910012"

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

//if err != nil -> log.Fatal(err)
func check(err error) {
	if err != nil {
		log.Fatal(err)
	}
}

func dumpConf(conf config) {
	data, err := json.MarshalIndent(&conf, "", "  ")
	check(err)
	err = ioutil.WriteFile(configFile, data, 0644)
	check(err)
}

func init() {
	if !ex.ExistDir(uploadDir) {
		err := ex.MakeDir(uploadDir)
		if err != nil {
			log.Fatal(err)
		}
	}
	if ex.ExistFile(configFile) {
		data, err := ex.ReadFileFull(configFile)
		check(err)
		err = json.Unmarshal(data, &conf)
		check(err)
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
		log.Fatal("Файл конфигурации не найден. Создан новый файл конфигурации.")
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
	if !cl.validOnServer(cl.conn) {
		log.Fatal("Valid on Server")
	}
	err := cn.SendString(conf.ClientId, cl.conn)
	cn.ReadSync(cl.conn)
	jsonData, err := json.Marshal(cl)
	err = cn.SendBytesWithDelim(jsonData, cl.conn)
	if err != nil {
		return err
	}
	q, err := cn.ReadQuery(cl.conn)
	if err != nil {
		return err
	}
	switch q.Method {
	case "wrong version":
		log.Fatal("wrong version")
	case "connect":
		return nil
	case "new id":
		err = cl.newId()
		if err != nil {
			return err
		}
	case "already exist":
		log.Fatal("already exist")
	}
	return nil
}

//Проходит проверку на подключение к серверу
func (cl *clientData) validOnServer(conn net.Conn) bool {
	var code = make([]byte, 16)
	bc, err := aes.NewCipher([]byte(key))
	err = cn.SendString(cl.Version, conn)
	if err != nil {
		return false
	}
	data, err := cn.ReadBytesByLen(16, conn)
	if err != nil {
		return false
	}
	bc.Decrypt(code, data)
	s := string(code)
	res := s[len(s)/2:] + s[:len(s)/2]
	bc.Encrypt(code, []byte(res))
	err = cn.SendBytes(code, conn)
	if err != nil {
		return false
	}
	mes, err := cn.ReadString(conn)
	if err != nil || mes != "ok" {
		return false
	}
	return true
}

//Получает новый id от сервера и сохраняет его
func (cl *clientData) newId() error {
	cn.SendSync(cl.conn)
	var err error
	conf.ClientId, err = cn.ReadString(cl.conn)
	if err != nil {
		return err
	}
	dumpConf(conf)
	fmt.Println("New Id: ", conf.ClientId)
	return nil
}

//Обрабатывает запрос на содержимое папки
func (cl *clientData) dir(path string) error {
	if path == "" {
		path = cl.BasePath
	}
	if ex.ExistDir(path) {
		res, err := ex.Explorer(path)
		if err != nil {
			err = cn.SendResponse(cn.Response{Err: err}, cl.conn)
			if err != nil {
				return err
			}
			return nil
		}
		res["nav"] = ex.NavFunc(path)
		data, err := json.Marshal(res)
		if err != nil {
			err = cn.SendResponse(cn.Response{Err: err}, cl.conn)
			if err != nil {
				return err
			}
			return nil
		}
		err = cn.SendResponse(cn.Response{DataLen: len(data)}, cl.conn)
		if err != nil {
			return err
		}
		cn.ReadSync(cl.conn)
		err = cn.SendBytes(data, cl.conn)
		if err != nil {
			return err
		}
	} else {
		err := cn.SendResponse(cn.Response{Err: errors.New(path + " is not exist")}, cl.conn)
		if err != nil {
			return err
		}
	}
	return nil
}

//Обрабатывает запрос на отправку файла
func (cl *clientData) fileFromClient(path string) error {
	if ex.ExistFile(path) {
		file, _ := os.Stat(path)
		err := cn.SendResponse(cn.Response{DataLen: int(file.Size()), Response: file.Name()}, cl.conn)
		if err != nil {
			return err
		}
		cn.ReadSync(cl.conn)
		err = cn.SendFile(path, cl.conn)
		if err != nil {
			return err
		}
	} else {
		err := cn.SendResponse(cn.Response{Err: errors.New(path + " is not exist")}, cl.conn)
		if err != nil {
			return err
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
				fmt.Println(err)
				break
			}
			err = json.Unmarshal(jsonEvents, &events)
			if err != nil {
				fmt.Println(err)
				break
			}
			eventsRun(events)
		} else {
			cn.ReadSync(s.conn)
		}
		for {
			s.Lock()
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
			fmt.Println(err)
			break
		}
		eventStatus, err = cn.ReadString(s.conn)
		if err != nil {
			fmt.Println(err)
			break
		}
		err = cn.SendBytes(imgB, s.conn)
		if err != nil {
			fmt.Println(err)
			break
		}
	}
	s.errTrue()
}

//Обрабатывает запрос на отправку файла
func (cl *clientData) stream(c chan error) {
	streamConn, err := net.Dial("tcp", conf.StreamServer)
	if err != nil {
		c <- errors.New("streamServer not found")
		return
	}
	if !cl.validOnServer(streamConn) {
		c <- errors.New("error valid stream server")
		return
	}
	err = cn.SendString(conf.ClientId, streamConn)
	if err != nil {
		c <- err
	}
	cn.ReadSync(streamConn)
	jsonData, err := json.Marshal(cl)
	if err != nil {
		c <- errors.New("json.Marshal(cl)")
		return
	}
	err = cn.SendBytesWithDelim(jsonData, streamConn)
	if err != nil {
		c <- err
		return
	}
	con, err := screenshot.Connect()
	if err != nil {
		c <- err
		return
	}
	defer screenshot.Close(con)
	ss := screenshot.ScreenSize(con)
	jsonSS, err := json.Marshal(ss)
	if err != nil {
		c <- errors.New("json.Marshal(ss)")
		return
	}
	cn.ReadSync(streamConn)
	err = cn.SendBytesWithDelim(jsonSS, streamConn)
	if err != nil {
		c <- err
		return
	}
	c <- nil
	s := &stream{conn: streamConn}
	go s.stream()
	for {
		img, err := screenshot.CaptureScreen(con)
		if err != nil {
			fmt.Println(err)
			break
		}
		buf := new(bytes.Buffer)
		err = jpeg.Encode(buf, img, &jpeg.Options{Quality: quality})
		if err != nil {
			fmt.Println(err)
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
	fmt.Println("stream stop by stream err")
}

func (cl *clientData) fileToClient(q cn.Query) error {
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
			return err
		}
		return err
	}
	err = cn.SendResponse(cn.Response{}, cl.conn)
	if err != nil {
		return err
	}
	return nil
}

//Принимает сообщения от сервера и обрабатывает их.
func worker(cl *clientData) error {
	for {
		q, err := cn.ReadQuery(cl.conn)
		if err != nil {
			return err
		}
		fmt.Printf("%#v %s \n", q, time.Now().Format("02.01.2006 15:04:05"))
		switch q.Method {
		case "testConnect":
			err = cn.SendResponse(cn.Response{}, cl.conn)
			if err != nil {
				return err
			}
		case "dir":
			err = cl.dir(q.Query)
			if err != nil {
				return err
			}
		case "fileFromClient":
			err = cl.fileFromClient(q.Query)
			if err != nil {
				return err
			}
		case "fileToClient":
			err = cl.fileToClient(q)
			if err != nil {
				return err
			}
		case "stream":
			channel := make(chan error)
			go cl.stream(channel)
			err = <-channel
			if err != nil {
				err = cn.SendResponse(cn.Response{Err: err}, cl.conn)
				if err != nil {
					return err
				}
			}
			err = cn.SendResponse(cn.Response{}, cl.conn)
			if err != nil {
				return err
			}
		default:
			return errors.New("something wrong")
		}
	}
}

//Подключается к серверу, выводит ошибки
func main() {
	cl := newClient()
	for {
		conn, err := net.Dial("tcp", conf.TcpServer)
		if err != nil {
			fmt.Println("Server not found")
			time.Sleep(5 * time.Second)
			continue
		}
		cl.conn = conn
		err = cl.connect()
		if err != nil {
			fmt.Println(err)
			cl.conn.Close()
			continue
		}
		fmt.Printf("Client connected %s \n", time.Now().Format("02.01.2006 15:04:05"))
		err = worker(cl)
		if err != nil {
			fmt.Println(err)
			cl.conn.Close()
			if fmt.Sprint(err) == "something wrong" {
				log.Fatal("something wrong")
			}
		}
	}
}
