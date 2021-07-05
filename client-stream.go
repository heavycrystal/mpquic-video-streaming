package main

import (
	"bufio"
	"encoding/hex"
	"fmt"
	"io"
	"os"

	"strconv"
	"strings"
	"time"

	"crypto/sha256"
	"crypto/tls"

	libvlc "github.com/adrg/libvlc-go/v3"
	quic "github.com/lucas-clemente/quic-go"
)

const addr = "100.0.0.1:4242"
const client_subfolder = "client-fs/"
const progress_spinner_delay = 100
const buffer_size = 1000

var segment_lookahead int = 10
var segment_lookbehind int = 5
var segment_count int
var current_stream int
var video_file_name string

func error_unwrap(e error) {
	if e != nil {
		panic(e)
	}
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

func parse_manifest(video_file_name string, client_id int64) (int, int, []int, []string) {

	manifest_handle, err := os.Open(client_subfolder + strconv.FormatInt(client_id, 10) + "/" + video_file_name + ".mfest")
	error_unwrap(err)

	manifest_reader := bufio.NewScanner(manifest_handle)
	manifest_reader.Scan()
	segment_count, _ := strconv.Atoi(manifest_reader.Text())
	manifest_reader.Scan()
	segment_length, _ := strconv.Atoi(manifest_reader.Text())

	manifest_reader.Scan()
	resolutions_count, err := strconv.Atoi(manifest_reader.Text())
	manifest_reader.Scan()
	var stream_resolutions []int
	for i := 0; i < resolutions_count; i++ {
		temp, _ := strconv.Atoi(strings.Split(manifest_reader.Text(), "#")[0])
		stream_resolutions = append(stream_resolutions, temp)
		temp, _ = strconv.Atoi(strings.Split(manifest_reader.Text(), "#")[1])
		stream_resolutions = append(stream_resolutions, temp)
		manifest_reader.Scan()
	}

	var sha256_checksums []string
	for i := 0; i < (resolutions_count * segment_count); i++ {
		sha256_checksums = append(sha256_checksums, manifest_reader.Text())
		manifest_reader.Scan()
	}

	return segment_count, segment_length, stream_resolutions, sha256_checksums
}

func receive_manifest(stream quic.Stream, client_id int64) {

	file_name_buffer := make([]byte, 64)
	file_size_buffer := make([]byte, 64)

	_, err := stream.Read(file_size_buffer)
	error_unwrap(err)
	file_size, err := strconv.ParseInt(strings.Split(string(file_size_buffer), "$")[0], 10, 64)
	error_unwrap(err)
	_, err = stream.Read(file_name_buffer)
	file_name := strings.Split(string(file_name_buffer), "$")[0]

	new_file_handle, err := os.Create(client_subfolder + strconv.FormatInt(client_id, 10) +
		"/" + file_name)
	error_unwrap(err)
	defer new_file_handle.Close()

	var received_bytes int64
	for {
		if (file_size - received_bytes) <= buffer_size {

			recv, err := io.CopyN(new_file_handle, stream, (file_size - received_bytes))
			error_unwrap(err)

			stream.Read(make([]byte, (received_bytes+buffer_size)-file_size))
			received_bytes = received_bytes + recv

			break
		}
		_, err := io.CopyN(new_file_handle, stream, buffer_size)
		error_unwrap(err)

		received_bytes = received_bytes + buffer_size
	}

}

func receive_segment(stream quic.Stream, client_id int64, current_stream int, current_segment int, sha256_checksums []string) bool {

	file_name_buffer := make([]byte, 64)
	file_size_buffer := make([]byte, 64)

	_, err := stream.Read(file_size_buffer)
	error_unwrap(err)
	file_size, err := strconv.ParseInt(strings.Split(string(file_size_buffer), "$")[0], 10, 64)
	error_unwrap(err)
	_, err = stream.Read(file_name_buffer)
	file_name := strings.Split(string(file_name_buffer), "$")[0]

	new_file_handle, err := os.Create(client_subfolder + strconv.FormatInt(client_id, 10) +
		"/" + file_name)
	error_unwrap(err)
	defer new_file_handle.Close()

	var received_bytes int64
	for {
		if (file_size - received_bytes) <= buffer_size {

			recv, err := io.CopyN(new_file_handle, stream, (file_size - received_bytes))
			error_unwrap(err)

			stream.Read(make([]byte, (received_bytes+buffer_size)-file_size))
			received_bytes = received_bytes + recv

			break
		}
		_, err := io.CopyN(new_file_handle, stream, buffer_size)
		error_unwrap(err)

		received_bytes = received_bytes + buffer_size
	}

	file_hash := sha256.New()
	new_file_handle.Seek(0, io.SeekStart)
	_, err = io.Copy(file_hash, new_file_handle)
	os.Rename(client_subfolder+strconv.FormatInt(client_id, 10)+
		"/"+file_name, client_subfolder+strconv.FormatInt(client_id, 10)+
		"/"+video_file_name+".0"+strconv.FormatInt(int64(current_segment), 10)+".ts")
	return hex.EncodeToString(file_hash.Sum(nil)) == sha256_checksums[(current_stream*segment_count)+current_segment]
}

func build_forward_cache(client_id int64, stream quic.Stream, cache_build_channel chan int, segment_count int, sha256_checksums []string) {

	current_segment := 0

	for i := 0; i < segment_lookahead; i++ {
		stream.Write([]byte(strconv.FormatInt(int64(current_stream), 10) + "#" + strconv.FormatInt(int64(current_segment), 10)))
		if receive_segment(stream, client_id, current_stream, current_segment, sha256_checksums) == false {
			current_segment = current_segment - 1
			i--
		}
		current_segment = current_segment + 1
	}
	cache_build_channel <- 1

	for {
		<-cache_build_channel
		stream.Write([]byte(strconv.FormatInt(int64(current_stream), 10) + "#" + strconv.FormatInt(int64(current_segment), 10)))
		if receive_segment(stream, client_id, current_stream, current_segment, sha256_checksums) == false {
			for {
				stream.Write([]byte(strconv.FormatInt(int64(current_stream), 10) + "#" + strconv.FormatInt(int64(current_segment), 10)))
				if receive_segment(stream, client_id, current_stream, current_segment, sha256_checksums) == true {
					break
				}
			}
		}
		current_segment = current_segment + 1
		if current_segment == segment_count {
			break
		}
	}

	stream.Write([]byte("done"))
	return
}

func delete_backward_cache(video_file_name string, client_id int64, segment_count int, delete_segment_channel chan int) {

	current_deleted_segment := -(segment_lookbehind + 1)
	for {
		<-delete_segment_channel
		current_deleted_segment = current_deleted_segment + 1
		if current_deleted_segment >= 0 {
			err := os.Remove(client_subfolder + strconv.FormatInt(client_id, 10) + "/" + video_file_name + ".0" +
				strconv.FormatInt(int64(current_deleted_segment), 10) + ".ts")
			error_unwrap(err)
		}
	}

	return
}

func input_taker_loop() {
	for {
		reader := bufio.NewReader(os.Stdin)
		text, err := reader.ReadString('\n')
		error_unwrap(err)
		current_stream, err = strconv.Atoi(text[:len(text)-1])
		error_unwrap(err)
	}
}

func main() {

	quicConfig := &quic.Config{
		CreatePaths:      true,
		IdleTimeout:      time.Duration(3600) * time.Second,
		HandshakeTimeout: time.Duration(600) * time.Second,
	}
	thread_quit_channel := make(chan int)
	cache_build_channel := make(chan int)
	delete_segment_channel := make(chan int)

	fmt.Println("Buttermilk: a simple MP-QUIC based video streaming system written in Go")
	fmt.Println("This is the CLIENT side code.")
	fmt.Println("Warning: VLC is required to generate the HLS stream.")
	fmt.Println("Warning: The video must be encoded using H.264 for HLS streaming to function.")

	fmt.Print("Trying to connect to server at: ", addr, "...")
	go progress_spinner(thread_quit_channel)
	session, err := quic.DialAddr(addr, &tls.Config{InsecureSkipVerify: true}, quicConfig)
	error_unwrap(err)
	stream, err := session.AcceptStream()
	error_unwrap(err)
	thread_quit_channel <- 1
	defer stream.Close()

	client_id_buffer := make([]byte, 8)
	video_file_name_buffer := make([]byte, 64)
	_, err = stream.Read(client_id_buffer)
	error_unwrap(err)
	client_id, err := strconv.ParseInt(strings.Split(string(client_id_buffer), "$")[0], 10, 32)
	error_unwrap(err)
	os.Mkdir(client_subfolder+strconv.FormatInt(client_id, 10)+"/", 0777)
	_, err = stream.Read(video_file_name_buffer)
	error_unwrap(err)
	video_file_name = strings.Split(string(video_file_name_buffer), "$")[0]

	// receive manifest file
	receive_manifest(stream, client_id)
	receive_manifest(stream, client_id)
	lsegment_count, segment_length, resolution_data, sha256_checksums := parse_manifest(video_file_name, client_id)
	segment_count = lsegment_count
	fmt.Printf("Video name: %v\n", video_file_name)
	fmt.Printf("Processed stream manifest: %v segments of %v seconds duration.\n", segment_count, segment_length)
	fmt.Printf("\nAvailable video resolutions:\n")

	for i := 0; i < len(resolution_data)/2; i++ {
		fmt.Printf("Stream ID #%v: %v x %v\n", i, resolution_data[(2*i)], resolution_data[(2*i)+1])
	}
	fmt.Print("Choose the stream to be used. This can be adjusted dynamically.")
	reader := bufio.NewReader(os.Stdin)
	text, err := reader.ReadString('\n')
	error_unwrap(err)
	current_stream, err = strconv.Atoi(text[:len(text)-1])
	error_unwrap(err)
	if segment_count < segment_lookahead {
		segment_lookahead = segment_count
	}
	if segment_count < segment_lookbehind {
		segment_lookbehind = segment_count
	}

	go build_forward_cache(client_id, stream, cache_build_channel, segment_count, sha256_checksums)
	go delete_backward_cache(video_file_name, client_id, segment_count, delete_segment_channel)
	<-cache_build_channel
	err = libvlc.Init("--quiet")
	error_unwrap(err)
	defer libvlc.Release()

	player, err := libvlc.NewPlayer()
	defer func() {
		player.Stop()
		player.Release()
	}()
	pwd, err := os.Getwd()
	error_unwrap(err)
	media, err := player.LoadMediaFromPath(pwd + "/" + client_subfolder + strconv.FormatInt(client_id, 10) + "/" +
		video_file_name + ".m3u8")
	error_unwrap(err)
	defer media.Release()

	fmt.Printf("Initialized VLC player. Playing video file...")
	go progress_spinner(thread_quit_channel)
	go input_taker_loop()
	err = player.Play()
	error_unwrap(err)
	for i := 0; i < (segment_count - segment_lookahead); i++ {
		time.Sleep(time.Duration(segment_length) * time.Second)
		cache_build_channel <- 1
		delete_segment_channel <- 1
	}
	time.Sleep(time.Duration(segment_lookahead*segment_length) * time.Second)
	thread_quit_channel <- 1

	err = os.RemoveAll(client_subfolder + strconv.FormatInt(client_id, 10) + "/")
	error_unwrap(err)

}
