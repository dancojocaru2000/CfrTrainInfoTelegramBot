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

	viewInKaiBaseUrl = "https://kai.infotren.dcdev.ro/view-train.html"
)

func HandleTrainNumberCommand(ctx context.Context, trainNumber string, date time.Time, groupIndex int) *HandlerResponse {
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
		}
	case errors.Is(err, api.ServerError):
		log.Printf("ERROR: In handle train number: %s", err.Error())
		return &HandlerResponse{
			Message: &bot.SendMessageParams{
				Text: fmt.Sprintf("Unknown server error when searching for train %s.", trainNumber),
			},
		}
	default:
		log.Printf("ERROR: In handle train number: %s", err.Error())
		return nil
	}

	if len(trainData.Groups) == 1 {
		groupIndex = 0
	}

	kaiUrl, _ := url.Parse(viewInKaiBaseUrl)
	kaiUrlQuery := kaiUrl.Query()
	kaiUrlQuery.Add("train", trainData.Number)
	kaiUrlQuery.Add("date", trainData.Groups[0].Stations[0].Departure.ScheduleTime.Format(time.RFC3339))
	if groupIndex != -1 {
		kaiUrlQuery.Add("groupIndex", strconv.Itoa(groupIndex))
	}
	kaiUrl.RawQuery = kaiUrlQuery.Encode()

	message := bot.SendMessageParams{}
	if groupIndex == -1 {
		message.Text = fmt.Sprintf("Train %s %s contains multiple groups. Please choose one.", trainData.Rank, trainData.Number)
		replyButtons := make([][]models.InlineKeyboardButton, len(trainData.Groups)+1)
		for i := range replyButtons {
			if i == len(trainData.Groups) {
				replyButtons[i] = append(replyButtons[i], models.InlineKeyboardButton{
					Text: "Open in WebApp",
					URL:  kaiUrl.String(),
				})
			} else {
				group := &trainData.Groups[i]
				replyButtons[i] = append(replyButtons[i], models.InlineKeyboardButton{
					Text:         fmt.Sprintf("%s ➔ %s", group.Route.From, group.Route.To),
					CallbackData: fmt.Sprintf(TrainInfoChooseGroupCallbackQuery+"\x1b%s\x1b%d\x1b%d", trainNumber, date.Unix(), i),
				})
			}
		}
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
		message.ReplyMarkup = models.InlineKeyboardMarkup{
			InlineKeyboard: [][]models.InlineKeyboardButton{
				{
					models.InlineKeyboardButton{
						Text: "Open in WebApp",
						URL:  kaiUrl.String(),
					},
				},
			},
		}
	} else {
		message.Text = fmt.Sprintf("The status of the train %s %s is unknown.", trainData.Rank, trainData.Number)
		message.Entities = []models.MessageEntity{
			{
				Type:   models.MessageEntityTypeBold,
				Offset: 24,
				Length: len(fmt.Sprintf("%s %s", trainData.Rank, trainData.Number)),
			},
		}
		message.ReplyMarkup = models.InlineKeyboardMarkup{
			InlineKeyboard: [][]models.InlineKeyboardButton{
				{
					models.InlineKeyboardButton{
						Text: "Open in WebApp",
						URL:  kaiUrl.String(),
					},
				},
			},
		}
	}

	return &HandlerResponse{
		Message: &message,
	}
}
