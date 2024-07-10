# Radio Streaming Program
This Go program allows you to stream radio stations using the radio-browser.info API. It fetches a list of popular stations, lets you select one to listen to, and provides control commands to pause, resume, stop, or change the station.


# Features
- Fetches a list of the top 50 most clicked radio stations.
- Plays selected radio station streams.
- Supports pausing, resuming, stopping, and changing the stream.

# Installation

1. Clone the repository:
```bash
git clone https://github.com/dadilll/radio.git
cd radio-streaming
```
2. Install the required Go packages:
```bash
go get github.com/gordonklaus/portaudio

```
3. Build the program:
```bash
go build -o radio
```

# Usage
1. Run the program:
```bash
./radio
```

2. Follow the prompts to select a radio station to listen to.

3. Use the following commands to control the stream:
- pause: Pauses the stream.
- resume: Resumes the paused stream.
- stop: Stops the stream.
- change: Stops the current stream and lets you select a new station.

# Dependencies
- PortAudio
- FFmpeg
- Go standard library packages: bufio, encoding/binary, encoding/json, fmt, io, net/http, os, os/exec, sync