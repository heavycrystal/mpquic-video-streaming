package main

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"syscall"
	"time"

	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"math/big"
	"os/exec"
	"os/signal"

	quic "github.com/lucas-clemente/quic-go"
)

const addr = "0.0.0.0:4242"
const server_subfolder = "server-fs/"
const progress_spinner_delay = 100
const buffer_size = 1000

func error_unwrap(e error) {
	if e != nil {
		panic(e)
	}
}

func generate_tls_configuration() *tls.Config {
	key, err := rsa.GenerateKey(rand.Reader, 1024)
	error_unwrap(err)
	template := x509.Certificate{SerialNumber: big.NewInt(1)}
	certDER, err := x509.CreateCertificate(rand.Reader, &template, &template, &key.PublicKey, key)
	error_unwrap(err)

	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})

	tlsCert, err := tls.X509KeyPair(certPEM, keyPEM)
	error_unwrap(err)

	return &tls.Config{Certificates: []tls.Certificate{tlsCert}}
}

func progress_spinner(quit_channel chan int) {
	for {
		for _, element := range `-\|/` {
			fmt.Printf("%c\b", element)
			time.Sleep(progress_spinner_delay * time.Millisecond)
			select {
			case <-quit_channel:
				fmt.Println("Done!")
				return
			default:
				continue
			}
		}
	}
}

func string_length_fix(input_string string, length int) string {
	if len(input_string) < length {
		return input_string + "$" + strings.Repeat(" ", length-len(input_string)-1)
	} else {
		return input_string[:length]
	}
}

func send_file_with_metadata(stream quic.Stream, input_file_name string) {

	file_handle, err := os.Open(input_file_name)
	defer file_handle.Close()

	error_unwrap(err)
	file_info, err := file_handle.Stat()
	error_unwrap(err)

	file_size := string_length_fix(strconv.FormatInt(file_info.Size(), 10), 64)
	file_name := string_length_fix(file_info.Name(), 64)

	_, err = stream.Write([]byte(file_size))
	error_unwrap(err)
	_, err = stream.Write([]byte(file_name))
	error_unwrap(err)

	send_buffer := make([]byte, buffer_size)
	total_sent_size := 0

	for {
		sent_size, err := file_handle.Read(send_buffer)
		if err == io.EOF {
			break
		} else if err != nil {
			error_unwrap(err)
		}
		total_sent_size = total_sent_size + sent_size
		_, err = stream.Write(send_buffer)
		error_unwrap(err)
	}
}

func worker_thread(session quic.Session, quit_channel chan int, thread_id int, video_file_name string) {

	stream, err := session.OpenStream()
	error_unwrap(err)
	defer session.Close(err)
	error_unwrap(err)
	defer stream.Close()

	client_id := string_length_fix(strconv.FormatInt(int64(thread_id), 10), 8)
	_, err = stream.Write([]byte(client_id))
	error_unwrap(err)
	video_name := string_length_fix(video_file_name, 64)
	_, err = stream.Write([]byte(video_name))
	//dispatch video manifest
	send_file_with_metadata(stream, server_subfolder+video_file_name+"-hls_stream/"+
		video_file_name+".m3u8")
	send_file_with_metadata(stream, server_subfolder+video_file_name+"-hls_stream/"+
		video_file_name+".mfest")

	command_buffer := make([]byte, 9)

	for {

		_, err = stream.Read(command_buffer)
		error_unwrap(err)

		if string(bytes.Trim(command_buffer, "\x00")) == "done" {
			break
		}

		send_file_with_metadata(stream, server_subfolder+video_file_name+"-hls_stream/"+
			video_file_name+"."+strings.Split(string(command_buffer), "#")[0]+strings.Split(string(bytes.Trim(command_buffer, "\x00")), "#")[1]+".ts")
	}

	fmt.Printf("\nClient #%v exited normally.", thread_id)
	return
}

func client_handler(listener quic.Listener, quit_channel chan int, video_file_name string) {

	active_worker_threads := 0
	worker_quit_channel := make(chan int)

	for {
		select {
		case <-quit_channel:
			for i := 0; i < active_worker_threads; i++ {
				worker_quit_channel <- 1
			}
			return
		default:
			session, err := listener.Accept()
			error_unwrap(err)
			fmt.Printf("\nClient #%v connected. Spawning worker thread.", active_worker_threads+1)
			go worker_thread(session, worker_quit_channel, active_worker_threads+1, video_file_name)
			active_worker_threads = active_worker_threads + 1
		}
	}
}

func main() {

	signal_channel := make(chan os.Signal)
	signal.Notify(signal_channel, syscall.SIGINT)

	reader := bufio.NewReader(os.Stdin)
	fmt.Println("Buttermilk: a simple MP-QUIC based video streaming system written in Go")
	fmt.Println("This is the SERVER side code.")
	fmt.Println("Warning: FFMPEG is required to generate the HLS stream.")
	fmt.Println("Warning: The video must be encoded using H.264 for HLS streaming to function.")
	fmt.Println("Warning: The file must be present in the server-fs/ subfolder of the current directory")
	fmt.Print("Filename of the video: ")
	text, err := reader.ReadString('\n')
	error_unwrap(err)
	video_file_name := text[:len(text)-1]
	thread_quit_channel := make(chan int)

	fmt.Print("Pregenerating HLS stream data using FFMPEG. This might take some time...")
	go progress_spinner(thread_quit_channel)
	python_call := exec.Command("python3", "gen_stream.py", video_file_name)
	python_output, err := python_call.Output()
	error_unwrap(err)
	if string(python_output) == "cached\n" {
		fmt.Print("already cached ")
	}
	thread_quit_channel <- 1

	quicConfig := &quic.Config{
		CreatePaths:      true,
		IdleTimeout:      time.Duration(3600) * time.Second,
		HandshakeTimeout: time.Duration(600) * time.Second,
	}
	fmt.Print("Attaching to: ", addr, "...")
	listener, err := quic.ListenAddr(addr, generate_tls_configuration(), quicConfig)
	error_unwrap(err)
	fmt.Println("Done!")
	fmt.Println("")
	fmt.Println("Server online! Now streaming: ", video_file_name)
	fmt.Print("Press Ctrl+C to stop streaming.")
	go client_handler(listener, thread_quit_channel, video_file_name)
	go progress_spinner(thread_quit_channel)

	<-signal_channel
	thread_quit_channel <- 1
	fmt.Printf("\nKill code confirmed.\n")
	fmt.Print("Cleaning up working directory...")
	go progress_spinner(thread_quit_channel)
	// os.RemoveAll(server_subfolder + video_file_name + "-hls_stream/")
	thread_quit_channel <- 1
	fmt.Println("Goodbye!")
}
