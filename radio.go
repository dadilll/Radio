package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/faiface/beep"
	"github.com/faiface/beep/effects"
	"github.com/faiface/beep/flac"
	"github.com/faiface/beep/mp3"
	"github.com/faiface/beep/speaker"
	"github.com/faiface/beep/vorbis"
	"github.com/faiface/beep/wav"
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

type Station struct {
	Name string `json:"name"`
	URL  string `json:"url"`
}

const favoritesFile = "favorites.json"

func loadFavoriteStations() ([]Station, error) {
	if _, err := os.Stat(favoritesFile); os.IsNotExist(err) {
		return nil, fmt.Errorf("файл избранного не существует")
	}
	data, err := os.ReadFile(favoritesFile)
	if err != nil {
		return nil, fmt.Errorf("не удалось прочитать файл избранного: %v", err)
	}
	var stations []Station
	err = json.Unmarshal(data, &stations)
	if err != nil {
		return nil, fmt.Errorf("не удалось разобрать данные избранного: %v", err)
	}
	return stations, nil
}

func saveStationsToFavorites(stations []Station) error {
	data, err := json.Marshal(stations)
	if err != nil {
		return fmt.Errorf("не удалось преобразовать станции: %v", err)
	}
	err = os.WriteFile(favoritesFile, data, 0644)
	if err != nil {
		return fmt.Errorf("не удалось записать файл избранного: %v", err)
	}
	return nil
}

func fetchStations() ([]Station, error) {
	resp, err := http.Get("https://de1.api.radio-browser.info/json/stations/topclick/50")
	if err != nil {
		return nil, fmt.Errorf("не удалось получить станции: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("плохой статус: %s", resp.Status)
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

func isStreamAvailable(url string) bool {
	cmd := exec.Command("ffmpeg", "-i", url, "-t", "5", "-f", "null", "-")
	err := cmd.Run()
	return err == nil
}

func updateStationsPeriodically(interval time.Duration, stations *[]Station) {
	ticker := time.NewTicker(interval)
	for {
		<-ticker.C
		newStations, err := fetchStations()
		if err == nil {
			*stations = newStations
		}
	}
}

var volume = &effects.Volume{
	Base:   2,
	Volume: 0,
	Silent: false,
}

func playStream(url string, control chan string, wg *sync.WaitGroup) error {
	defer wg.Done()

	resp, err := http.Get(url)
	if err != nil {
		return fmt.Errorf("не удалось получить поток: %v", err)
	}
	defer resp.Body.Close()

	streamer, format, err := decodeAudio(resp.Body)
	if err != nil {
		return fmt.Errorf("не удалось декодировать аудио: %v", err)
	}

	speaker.Init(format.SampleRate, format.SampleRate.N(time.Second/10))

	volume.Streamer = streamer
	speaker.Play(volume)

	done := make(chan bool)
	go func() {
		<-done
		resp.Body.Close()
	}()

	for {
		select {
		case cmd := <-control:
			switch cmd {
			case "pause":
				speaker.Lock()
				volume.Silent = true
				speaker.Unlock()
			case "resume":
				speaker.Lock()
				volume.Silent = false
				speaker.Unlock()
			case "stop":
				done <- true
				return nil
			case "volume_up":
				speaker.Lock()
				volume.Volume += 0.1
				speaker.Unlock()
			case "volume_down":
				speaker.Lock()
				volume.Volume -= 0.1
				speaker.Unlock()
			}
		}
	}
}

type readCloser struct {
	io.Reader
}

func (rc readCloser) Close() error {
	return nil
}

func decodeAudio(r io.Reader) (beep.StreamSeekCloser, beep.Format, error) {
	rc := readCloser{r}
	var streamer beep.StreamSeekCloser
	var format beep.Format
	var err error

	if streamer, format, err = mp3.Decode(rc); err == nil {
		return streamer, format, nil
	}
	if streamer, format, err = flac.Decode(rc); err == nil {
		return streamer, format, nil
	}
	if streamer, format, err = wav.Decode(rc); err == nil {
		return streamer, format, nil
	}
	if streamer, format, err = vorbis.Decode(rc); err == nil {
		return streamer, format, nil
	}

	return nil, beep.Format{}, fmt.Errorf("неподдерживаемый формат")
}

func updateEqualizer(equalizer *tview.TextView, volumeLevel float64, stationName string) {
	barCount := 10
	fullBars := int(volumeLevel * float64(barCount))
	bars := strings.Repeat("[green]█", fullBars) + strings.Repeat("[grey]█", barCount-fullBars)
	equalizer.SetText(fmt.Sprintf("Громкость: %.1f\nСтанция: %s\n%s", volumeLevel, stationName, bars))
}

func main() {
	stations, err := fetchStations()
	if err != nil {
		fmt.Fprintln(os.Stderr, "Ошибка загрузки списка станций:", err)
		os.Exit(1)
	}

	favorites, err := loadFavoriteStations()
	if err != nil {
		favorites = []Station{}
	}

	app := tview.NewApplication()
	list := tview.NewList()
	list.SetTitle("Доступные станции").SetBorder(true)
	for i, station := range stations {
		list.AddItem(station.Name, "", rune('0'+i%10), nil)
	}

	favoritesList := tview.NewList()
	favoritesList.SetTitle("Избранные станции").SetBorder(true)
	for i, station := range favorites {
		favoritesList.AddItem(station.Name, "", rune('0'+i%10), nil)
	}

	info := tview.NewTextView().SetText("Выберите станцию для прослушивания").SetDynamicColors(true).SetRegions(true).SetWrap(true)
	info.SetBorder(true).SetTitle("Информация")

	equalizer := tview.NewTextView().SetDynamicColors(true).SetWrap(true)
	equalizer.SetBorder(true).SetTitle("Эквалайзер")

	currentStation := ""
	updateEqualizer(equalizer, volume.Volume, currentStation)

	flex := tview.NewFlex().
		AddItem(list, 0, 1, true).
		AddItem(info, 0, 2, false).
		AddItem(equalizer, 0, 2, false)

	var control chan string
	var wg sync.WaitGroup

	list.SetChangedFunc(func(index int, mainText string, secondaryText string, shortcut rune) {
		info.SetText(fmt.Sprintf("Станция: %s\nURL: %s", stations[index].Name, stations[index].URL))
	})

	list.SetSelectedFunc(func(index int, mainText string, secondaryText string, shortcut rune) {
		selectedStation := stations[index]
		if !isStreamAvailable(selectedStation.URL) {
			info.SetText(fmt.Sprintf("[red]Станция недоступна: %s", selectedStation.Name))
			return
		}

		currentStation = selectedStation.Name
		updateEqualizer(equalizer, volume.Volume, currentStation)

		control = make(chan string)
		wg.Add(1)
		go func() {
			err := playStream(selectedStation.URL, control, &wg)
			if err != nil {
				fmt.Fprintln(os.Stderr, "Ошибка во время прослушивания:", err)
			}
		}()

		info.SetText(fmt.Sprintf("[green]Играет: %s", selectedStation.Name))
	})

	favoritesList.SetSelectedFunc(func(index int, mainText string, secondaryText string, shortcut rune) {
		selectedStation := favorites[index]
		if !isStreamAvailable(selectedStation.URL) {
			info.SetText(fmt.Sprintf("[red]Станция недоступна: %s", selectedStation.Name))
			return
		}

		currentStation = selectedStation.Name
		updateEqualizer(equalizer, volume.Volume, currentStation)

		control = make(chan string)
		wg.Add(1)
		go func() {
			err := playStream(selectedStation.URL, control, &wg)
			if err != nil {
				fmt.Fprintln(os.Stderr, "Ошибка во время прослушивания:", err)
			}
		}()

		info.SetText(fmt.Sprintf("[green]Играет: %s", selectedStation.Name))
		app.SetRoot(flex, true).SetFocus(list)
	})

	go updateStationsPeriodically(10*time.Minute, &stations)

	app.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if control != nil {
			switch event.Key() {
			case tcell.KeyCtrlZ:
				control <- "volume_up"
				updateEqualizer(equalizer, volume.Volume, currentStation)
			case tcell.KeyCtrlX:
				control <- "volume_down"
				updateEqualizer(equalizer, volume.Volume, currentStation)
			case tcell.KeyLeft:
				speaker.Lock()
				volume.Volume = -2
				speaker.Unlock()
				updateEqualizer(equalizer, volume.Volume, currentStation)
			case tcell.KeyRight:
				speaker.Lock()
				volume.Volume = 2
				speaker.Unlock()
				updateEqualizer(equalizer, volume.Volume, currentStation)
			}
		}

		switch event.Rune() {
		case 'S', 's':
			index := list.GetCurrentItem()
			if index >= 0 && index < len(stations) {
				favorites = append(favorites, stations[index])
				saveStationsToFavorites(favorites)
				favoritesList.AddItem(stations[index].Name, "", rune('0'+len(favorites)%10), nil)
				info.SetText(fmt.Sprintf("[yellow]Станция добавлена в избранное: %s", stations[index].Name))
			}
		case 'D', 'd':
			index := favoritesList.GetCurrentItem()
			if index >= 0 && index < len(favorites) {
				favorites = append(favorites[:index], favorites[index+1:]...)
				saveStationsToFavorites(favorites)
				favoritesList.Clear()
				for i, station := range favorites {
					favoritesList.AddItem(station.Name, "", rune('0'+i%10), nil)
				}
				info.SetText("[yellow]Станция удалена из избранного")
			}
		case 'M', 'm':
			app.SetRoot(favoritesList, true).SetFocus(favoritesList)
		case 'B', 'b':
			app.SetRoot(flex, true).SetFocus(list)
		}

		return event
	})

	if err := app.SetRoot(flex, true).Run(); err != nil {
		panic(err)
	}
}
