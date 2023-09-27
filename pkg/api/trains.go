package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

type TrainResponse struct {
	Rank     string       `json:"rank"`
	Number   string       `json:"number"`
	Date     string       `json:"date"`
	Operator string       `json:"operator"`
	Groups   []TrainGroup `json:"groups"`
}

type TrainGroup struct {
	Route struct {
		From string `json:"from"`
		To   string `json:"to"`
	} `json:"route"`
	Status *struct {
		Delay   int    `json:"delay"`
		Station string `json:"station"`
		State   string `json:"state"`
	} `json:"status"`
	Stations []TrainStation `json:"stations"`
}

type TrainStation struct {
	Name         string       `json:"name"`
	LinkName     string       `json:"linkName"`
	Km           int          `json:"km"`
	StoppingTime *int         `json:"stoppingTime"`
	Platform     *string      `json:"platform"`
	Arrival      *TrainArrDep `json:"arrival"`
	Departure    *TrainArrDep `json:"departure"`
	Notes        []any        `json:"notes"`
}

type TrainArrDep struct {
	ScheduleTime time.Time `json:"scheduleTime"`
	Status       *struct {
		Delay     int  `json:"delay"`
		Real      bool `json:"real"`
		Cancelled bool `json:"cancelled"`
	} `json:"status"`
}

const (
	trainApiEndpoint = "https://scraper.infotren.dcdev.ro/v3"
)

var (
	TrainNotFound = fmt.Errorf("train not found")
	ServerError   = fmt.Errorf("server error")
)

func GetTrain(ctx context.Context, trainNumber string, date time.Time) (*TrainResponse, error) {
	u, _ := url.Parse(trainApiEndpoint)
	u.Path, _ = url.JoinPath(u.Path, "trains", trainNumber)
	query := u.Query()
	query.Add("date", date.Format(time.RFC3339))
	u.RawQuery = query.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("error getting train %s: %w", trainNumber, err)
	}

	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("error getting train %s: %w", trainNumber, err)
	}
	defer func() {
		_ = res.Body.Close()
	}()

	switch {
	case res.StatusCode == http.StatusNotFound:
		return nil, fmt.Errorf("error getting train %s: %w", trainNumber, TrainNotFound)
	case res.StatusCode/100 != 2:
		return nil, fmt.Errorf("error getting train %s: status code %d: %w", trainNumber, res.StatusCode, ServerError)
	}

	var body []byte
	if res.ContentLength > 0 {
		body = make([]byte, res.ContentLength)
		n, err := io.ReadFull(res.Body, body)
		if err != nil && err != io.EOF {
			return nil, fmt.Errorf("error getting train %s: %w", trainNumber, err)
		} else if n != int(res.ContentLength) {
			body = body[0:n]
		}
	} else {
		body, err = io.ReadAll(res.Body)
		if err != nil {
			return nil, fmt.Errorf("error getting train %s: %w", trainNumber, err)
		}
	}

	var trainData TrainResponse
	if err := json.Unmarshal(body, &trainData); err != nil {
		return nil, fmt.Errorf("error getting train %s: %w", trainNumber, err)
	}

	return &trainData, nil
}
