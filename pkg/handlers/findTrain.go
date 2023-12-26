package handlers

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/url"
	"strconv"
	"strings"
	"time"

	"dcdev.ro/CfrTrainInfoTelegramBot/pkg/api"
	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
)

const (
	TrainInfoChooseDateCallbackQuery  = "TI_CHOOSE_DATE"
	TrainInfoChooseGroupCallbackQuery = "TI_CHOOSE_GROUP"
	TrainInfoSubscribeCallbackQuery   = "TI_SUB"
	TrainInfoUnsubscribeCallbackQuery = "TI_UNSUB"

	viewInKaiBaseUrl = "https://kai.infotren.dcdev.ro/view-train.html"

	subscribeButton    = "Subscribe to updates"
	unsubscribeButton  = "Unsubscribe from updates"
	openInWebAppButton = "Open in WebApp"
)

const (
	TrainInfoResponseButtonExcludeSub = iota
	TrainInfoResponseButtonIncludeSub
	TrainInfoResponseButtonIncludeUnsub
)

func HandleTrainNumberCommand(ctx context.Context, trainNumber string, date time.Time, groupIndex int, isSubscribed bool) (*HandlerResponse, bool) {
	trainData, err := api.GetTrain(ctx, trainNumber, date)

	switch {
	case err == nil:
		break
	case errors.Is(err, api.TrainNotFound):
		log.Printf("ERROR: In handle train number: %s", err.Error())
		return &HandlerResponse{
			Message: &bot.SendMessageParams{
				Text: fmt.Sprintf("The train %s was not found.", trainNumber),
			},
			ShouldUnsubscribe: func() bool {
				now := time.Now().In(utils.Location)
				midnightYesterday := time.Date(now.Year(), now.Month(), now.Day()-1, 0, 0, 0, 0, utils.Location)
				return date.Before(midnightYesterday)
			}(),
		}, false
	case errors.Is(err, api.ServerError):
		log.Printf("ERROR: In handle train number: %s", err.Error())
		return &HandlerResponse{
			Message: &bot.SendMessageParams{
				Text: fmt.Sprintf("Unknown server error when searching for train %s.", trainNumber),
			},
			ShouldUnsubscribe: func() bool {
				now := time.Now().In(utils.Location)
				midnightYesterday := time.Date(now.Year(), now.Month(), now.Day()-1, 0, 0, 0, 0, utils.Location)
				return date.Before(midnightYesterday)
			}(),
		}, false
	default:
		log.Printf("ERROR: In handle train number: %s", err.Error())
		return nil, false
	}

	if len(trainData.Groups) == 1 {
		groupIndex = 0
	}

	shouldUnsubscribe := func() bool {
		if len(trainData.Groups) <= groupIndex {
			groupIndex = 0
		}
		now := time.Now().In(utils.Location)
		midnightYesterday := time.Date(now.Year(), now.Month(), now.Day()-1, 0, 0, 0, 0, utils.Location)
		lastStation := trainData.Groups[groupIndex].
			Stations[len(trainData.Groups[groupIndex].Stations)-1]
		if now.After(lastStation.Arrival.
			ScheduleTime.Add(time.Hour * 6)) {
			return true
		}
		if trainData.Groups[groupIndex].
			Status != nil && trainData.Groups[groupIndex].
			Status.Station == lastStation.Name &&
			trainData.Groups[groupIndex].Status.
				State == "arrival" {
			return true
		}
		if date.Before(midnightYesterday) {
			return true
		}

		return false
	}()

	message := bot.SendMessageParams{}
	if groupIndex == -1 {
		message.Text = fmt.Sprintf("Train %s %s contains multiple groups. Please choose one.", trainData.Rank, trainData.Number)
		replyButtons := make([][]models.InlineKeyboardButton, 0, len(trainData.Groups)+1)
		for i, group := range trainData.Groups {
			replyButtons = append(replyButtons, []models.InlineKeyboardButton{
				{
					Text:         fmt.Sprintf("%s ➔ %s", group.Route.From, group.Route.To),
					CallbackData: fmt.Sprintf(TrainInfoChooseGroupCallbackQuery+"\x1b%s\x1b%d\x1b%d", trainNumber, date.Unix(), i),
				},
			})
		}
		kaiUrl, _ := url.Parse(viewInKaiBaseUrl)
		kaiUrlQuery := kaiUrl.Query()
		kaiUrlQuery.Add("train", trainData.Number)
		kaiUrlQuery.Add("date", trainData.Groups[0].Stations[0].Departure.ScheduleTime.Format(time.RFC3339))
		kaiUrl.RawQuery = kaiUrlQuery.Encode()
		replyButtons = append(replyButtons, []models.InlineKeyboardButton{
			{
				Text: "Open in WebApp",
				URL:  kaiUrl.String(),
			},
		})
		message.ReplyMarkup = models.InlineKeyboardMarkup{
			InlineKeyboard: replyButtons,
		}
	} else if len(trainData.Groups) > groupIndex {
		group := &trainData.Groups[groupIndex]

		messageText := strings.Builder{}
		messageText.WriteString(fmt.Sprintf("Train %s %s\n%s ➔ %s\n\n", trainData.Rank, trainData.Number, group.Route.From, group.Route.To))

		messageText.WriteString(fmt.Sprintf("Date: %s\n", trainData.Date))
		messageText.WriteString(fmt.Sprintf("Operator: %s\n", trainData.Operator))
		if group.Status != nil {
			messageText.WriteString("Status: ")
			if group.Status.Delay == 0 {
				messageText.WriteString("on time when ")
			} else {
				messageText.WriteString(fmt.Sprintf("%d min ", func(x int) int {
					if x < 0 {
						return -x
					} else {
						return x
					}
				}(group.Status.Delay)))
				if group.Status.Delay < 0 {
					messageText.WriteString("early when ")
				} else {
					messageText.WriteString("late when ")
				}
			}
			switch group.Status.State {
			case "arrival":
				messageText.WriteString("arriving at ")
			case "departure":
				messageText.WriteString("departing from ")
			case "passing":
				messageText.WriteString("passing through ")
			}
			messageText.WriteString(group.Status.Station)
			messageText.WriteString("\n")
		}

		message.Text = messageText.String()
		message.Entities = []models.MessageEntity{
			{
				Type:   models.MessageEntityTypeBold,
				Offset: 6,
				Length: len(fmt.Sprintf("%s %s", trainData.Rank, trainData.Number)),
			},
		}
		buttonKind := TrainInfoResponseButtonIncludeSub
		if shouldUnsubscribe {
			buttonKind = TrainInfoResponseButtonExcludeSub
		} else if isSubscribed {
			buttonKind = TrainInfoResponseButtonIncludeUnsub
		}
		message.ReplyMarkup = GetTrainNumberCommandResponseButtons(trainData.Number, group.Stations[0].Departure.ScheduleTime, groupIndex, buttonKind)
	} else {
		message.Text = fmt.Sprintf("The status of the train %s %s is unknown.", trainData.Rank, trainData.Number)
		message.Entities = []models.MessageEntity{
			{
				Type:   models.MessageEntityTypeBold,
				Offset: 24,
				Length: len(fmt.Sprintf("%s %s", trainData.Rank, trainData.Number)),
			},
		}
		message.ReplyMarkup = GetTrainNumberCommandResponseButtons(trainData.Number, trainData.Groups[0].Stations[0].Departure.ScheduleTime, groupIndex, TrainInfoResponseButtonExcludeSub)
	}

	return &HandlerResponse{
		Message:           &message,
		ShouldUnsubscribe: shouldUnsubscribe,
	}, true
}

func GetTrainNumberCommandResponseButtons(trainNumber string, date time.Time, groupIndex int, responseButton int) models.ReplyMarkup {
	kaiUrl, _ := url.Parse(viewInKaiBaseUrl)
	kaiUrlQuery := kaiUrl.Query()
	kaiUrlQuery.Add("train", trainNumber)
	kaiUrlQuery.Add("date", date.Format(time.RFC3339))
	if groupIndex != -1 {
		kaiUrlQuery.Add("groupIndex", strconv.Itoa(groupIndex))
	}
	kaiUrl.RawQuery = kaiUrlQuery.Encode()

	result := make([][]models.InlineKeyboardButton, 0)
	if responseButton == TrainInfoResponseButtonIncludeSub {
		result = append(result, []models.InlineKeyboardButton{
			{
				Text:         subscribeButton,
				CallbackData: fmt.Sprintf(TrainInfoSubscribeCallbackQuery+"\x1b%s\x1b%d\x1b%d", trainNumber, date.Unix(), groupIndex),
			},
		})
	} else if responseButton == TrainInfoResponseButtonIncludeUnsub {
		result = append(result, []models.InlineKeyboardButton{
			{
				Text:         unsubscribeButton,
				CallbackData: fmt.Sprintf(TrainInfoUnsubscribeCallbackQuery+"\x1b%s\x1b%d\x1b%d", trainNumber, date.Unix(), groupIndex),
			},
		})
	}
	result = append(result, []models.InlineKeyboardButton{
		{
			Text: openInWebAppButton,
			URL:  kaiUrl.String(),
		},
	})
	return models.InlineKeyboardMarkup{
		InlineKeyboard: result,
	}
}
