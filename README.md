# mpquic-video-streaming
A client-server video streaming platform using the MPQUIC protocol. Written in the Go programming language and uses libVLC and FFMPEG.

Instructions:

1. Get the modified version of quic-go, https://github.com/qdeconinck/mp-quic and set it up accordingly.

2. Setup a client-fs/ and server-fs/ directory in the root of the repository and add video files to the server-fs/ directory.

3. Use the gen-stream.py file to generate HLS stream data from the video files.

4. Start the server and client files as seperate instances. A file is server from server-fs/ to client-fs/
