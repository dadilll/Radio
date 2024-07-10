package main

import (
	"bufio"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"sync"

	"github.com/gordonklaus/portaudio"
)

// Station represents a radio station with name and URL.
type Station struct {
	Name string `json:"name"`
	URL  string `json:"url"`
}

// fetchStations fetches radio stations from the radio-browser.info API.
func fetchStations() ([]Station, error) {
	resp, err := http.Get("https://de1.api.radio-browser.info/json/stations/topclick/50")
	if err != nil {
		return nil, fmt.Errorf("could not fetch stations: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("bad status: %s", resp.Status)
	}

	var data []struct {
		Name string `json:"name"`
		URL  string `json:"url"`
	}
	err = json.NewDecoder(resp.Body).Decode(&data)
	if err != nil {
		return nil, err
	}

	var stations []Station
	for _, station := range data {
		stations = append(stations, Station{Name: station.Name, URL: station.URL})
	}

	return stations, nil
}

func playStream(url string, control chan string, wg *sync.WaitGroup) error {
	defer wg.Done()

	cmd := exec.Command("ffmpeg", "-loglevel", "error", "-i", url, "-f", "s16le", "-ar", "44100", "-ac", "2", "pipe:1")
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("could not get stdout pipe: %v", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("could not get stderr pipe: %v", err)
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("could not start ffmpeg: %v", err)
	}

	go func() {
		scanner := bufio.NewScanner(stderr)
		for scanner.Scan() {
			fmt.Fprintln(os.Stderr, scanner.Text())
		}
	}()

	portaudio.Initialize()
	defer portaudio.Terminate()

	stream, err := portaudio.OpenDefaultStream(0, 2, 44100, 0, func(out []float32) {
		buf := make([]byte, len(out)*2)
		_, err := io.ReadFull(stdout, buf)
		if err != nil {
			for i := range out {
				out[i] = 0
			}
			return
		}
		for i := 0; i < len(out)/2; i++ {
			out[2*i] = float32(int16(binary.LittleEndian.Uint16(buf[4*i:]))) / 32768.0
			out[2*i+1] = float32(int16(binary.LittleEndian.Uint16(buf[4*i+2:]))) / 32768.0
		}
	})
	if err != nil {
		return fmt.Errorf("could not open default stream: %v", err)
	}
	defer stream.Close()

	if err := stream.Start(); err != nil {
		return fmt.Errorf("could not start stream: %v", err)
	}

	done := make(chan bool)
	go func() {
		<-done
		cmd.Process.Kill()
	}()

	for {
		select {
		case cmd := <-control:
			switch cmd {
			case "pause":
				if err := stream.Stop(); err != nil {
					return fmt.Errorf("could not stop stream: %v", err)
				}
			case "resume":
				if err := stream.Start(); err != nil {
					return fmt.Errorf("could not start stream: %v", err)
				}
			case "stop":
				done <- true
				if err := stream.Stop(); err != nil {
					return fmt.Errorf("could not stop stream: %v", err)
				}
				return nil
			}
		}
	}
}

func main() {
	stations, err := fetchStations()
	if err != nil {
		fmt.Fprintln(os.Stderr, "Ошибка загрузки списка станций:", err)
		os.Exit(1)
	}

	fmt.Println("Доступные станции:")
	for i, station := range stations {
		fmt.Printf("%d: %s\n", i+1, station.Name)
	}

	var stationIndex int
	for {
		fmt.Print("Выберите номер станции для прослушивания: ")
		_, err = fmt.Scan(&stationIndex)
		if err != nil || stationIndex < 1 || stationIndex > len(stations) {
			fmt.Println("Неверный номер станции.")
			continue
		}

		selectedStation := stations[stationIndex-1]
		fmt.Printf("Вы выбрали станцию: %s\n", selectedStation.Name)

		control := make(chan string)
		var wg sync.WaitGroup
		wg.Add(1)
		go func() {
			err := playStream(selectedStation.URL, control, &wg)
			if err != nil {
				fmt.Fprintln(os.Stderr, "Ошибка во время прослушивания:", err)
			}
		}()

		exitInnerLoop := false
		for !exitInnerLoop {
			fmt.Println("Введите команду (pause/resume/stop/change): ")
			var cmd string
			_, err := fmt.Scan(&cmd)
			if err != nil {
				fmt.Println("Ошибка ввода команды.")
				continue
			}
			switch cmd {
			case "pause", "resume", "stop":
				control <- cmd
				if cmd == "stop" {
					wg.Wait()
					exitInnerLoop = true
				}
			case "change":
				control <- "stop"
				wg.Wait()
				exitInnerLoop = true
			default:
				fmt.Println("Неизвестная команда.")
			}
		}
	}
}
