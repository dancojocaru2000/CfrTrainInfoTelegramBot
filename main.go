package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"dcdev.ro/CfrTrainInfoTelegramBot/pkg/database"
	"dcdev.ro/CfrTrainInfoTelegramBot/pkg/handlers"
	"dcdev.ro/CfrTrainInfoTelegramBot/pkg/subscriptions"
	"dcdev.ro/CfrTrainInfoTelegramBot/pkg/utils"
	tgBot "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

const (
	trainInfoCommand   = "/train_info"
	stationInfoCommand = "/station_info"
	routeCommand       = "/route"
	cancelCommand      = "/cancel"

	initialMessage = `Hello. 😄

You can send the following commands:

` + trainInfoCommand + ` - Find information about a certain train.
` + stationInfoCommand + ` - Find departures or arrivals at a certain station.
` + routeCommand + ` - Find trains for a certain route.

You may use ` + cancelCommand + ` to cancel any ongoing command.`
	waitingForTrainNumberMessage = "Please send the number of the train you want information for."
	pleaseWaitMessage            = "Please wait..."
	cancelResponseMessage        = "Command cancelled."
	chooseDateMessage            = `Please choose the date of departure from the first station for this train.

You may also send the date as a message in the following formats: dd.mm.yyyy, m/d/yyyy, yyyy-mm-dd, UNIX timestamp.

Keep in mind that, for night trains, this date might be yesterday.`
	invalidDateMessage = "Invalid date. Please try again or us " + cancelCommand + " to cancel."
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	log.SetOutput(os.Stderr)

	botToken := os.Getenv("CFR_BOT.TOKEN")
	botToken = strings.TrimSpace(botToken)
	if len(botToken) == 0 {
		log.Fatal("ERROR: No bot token supplied; supply with CFR_BOT.TOKEN")
	}

	db, err := gorm.Open(sqlite.Open("bot_db.sqlite"), &gorm.Config{})
	if err != nil {
		panic(err)
	}
	if err := db.AutoMigrate(&handlers.ChatFlow{}); err != nil {
		panic(err)
	}
	if err := db.AutoMigrate(&subscriptions.SubData{}); err != nil {
		panic(err)
	}
	database.SetDatabase(db)

	subs, err := subscriptions.LoadSubscriptions()
	if err != nil {
		subs = nil
		fmt.Printf("WARN : Could not load subscriptions: %s\n", err.Error())
	}

	go subs.CheckSubscriptions(ctx)

	bot, err := tgBot.New(botToken, tgBot.WithDefaultHandler(handlerBuilder(subs)))
	if err != nil {
		panic(err)
	}

	log.Print("INFO : Starting...")
	bot.Start(ctx)
}

func handlerBuilder(subs *subscriptions.Subscriptions) func(context.Context, *tgBot.Bot, *models.Update) {
	return func(ctx context.Context, b *tgBot.Bot, update *models.Update) {
		handler(ctx, b, update, subs)
	}
}

func handler(ctx context.Context, b *tgBot.Bot, update *models.Update, subs *subscriptions.Subscriptions) {
	var response *handlers.HandlerResponse
	var toEditId int
	defer func() {
		if response == nil {
			return
		}
		if response.ProgressMessageToEditId != 0 {
			toEditId = response.ProgressMessageToEditId
		}
		if response.Message != nil {
			response.Message.ChatID = response.Injected.ChatId
			if toEditId != 0 {
				b.EditMessageText(ctx, &tgBot.EditMessageTextParams{
					ChatID:                response.Message.ChatID,
					MessageID:             toEditId,
					Text:                  response.Message.Text,
					ParseMode:             response.Message.ParseMode,
					Entities:              response.Message.Entities,
					DisableWebPagePreview: response.Message.DisableWebPagePreview,
					ReplyMarkup:           response.Message.ReplyMarkup,
				})
			} else {
				b.SendMessage(ctx, response.Message)
			}
		}
		if response.CallbackAnswer != nil {
			b.AnswerCallbackQuery(ctx, response.CallbackAnswer)
		}
		for _, edit := range response.MessageEdits {
			if (edit.ChatID == nil || edit.MessageID == 0) && edit.InlineMessageID == "" {
				edit.ChatID = response.Injected.ChatId
				edit.MessageID = response.Injected.MessageId
			}
			b.EditMessageText(ctx, edit)
		}
		for _, edit := range response.MessageMarkupEdits {
			if (edit.ChatID == nil || edit.MessageID == 0) && edit.InlineMessageID == "" {
				edit.ChatID = response.Injected.ChatId
				edit.MessageID = response.Injected.MessageId
			}
			b.EditMessageReplyMarkup(ctx, edit)
		}
	}()

	if update.Message != nil {
		defer func() {
			if response == nil {
				response = &handlers.HandlerResponse{}
			}
			response.Injected.ChatId = update.Message.Chat.ID
			response.Injected.MessageId = update.Message.ID
		}()
		log.Printf("DEBUG: Got message: %s\n", update.Message.Text)

		chatFlow := handlers.GetChatFlow(update.Message.Chat.ID)

		switch {
		case strings.HasPrefix(update.Message.Text, trainInfoCommand):
			response = handleFindTrainStages(ctx, b, update, subs)
		case strings.HasPrefix(update.Message.Text, cancelCommand):
			handlers.SetChatFlow(chatFlow, handlers.InitialFlowType, handlers.InitialFlowType, "")
			response = &handlers.HandlerResponse{
				Message: &tgBot.SendMessageParams{
					Text: cancelResponseMessage,
				},
			}
		default:
			switch chatFlow.Type {
			case handlers.InitialFlowType:
				b.SendMessage(ctx, &tgBot.SendMessageParams{
					ChatID: update.Message.Chat.ID,
					Text:   initialMessage,
				})
			case handlers.TrainInfoFlowType:
				log.Printf("DEBUG: trainInfoFlowType with stage %s\n", chatFlow.Stage)
				response = handleFindTrainStages(ctx, b, update, subs)
			}
		}
	}
	if update.CallbackQuery != nil {
		defer func() {
			if response == nil {
				response = &handlers.HandlerResponse{
					CallbackAnswer: &tgBot.AnswerCallbackQueryParams{
						CallbackQueryID: update.CallbackQuery.ID,
					},
				}
			}
			response.Injected.ChatId = update.CallbackQuery.Message.Chat.ID
			response.Injected.MessageId = update.CallbackQuery.Message.ID
			if response.CallbackAnswer == nil {
				response.CallbackAnswer = &tgBot.AnswerCallbackQueryParams{
					CallbackQueryID: update.CallbackQuery.ID,
				}
			}
			if response.CallbackAnswer.CallbackQueryID == "" {
				response.CallbackAnswer.CallbackQueryID = update.CallbackQuery.ID
			}
		}()

		chatFlow := handlers.GetChatFlow(update.CallbackQuery.Message.Chat.ID)

		if len(update.CallbackQuery.Data) != 0 {
			splitted := strings.Split(update.CallbackQuery.Data, "\x1b")
			switch splitted[0] {
			case handlers.TrainInfoChooseDateCallbackQuery:
				trainNumber := splitted[1]
				dateInt, _ := strconv.ParseInt(splitted[2], 10, 64)
				date := time.Unix(dateInt, 0)
				message, err := b.SendMessage(ctx, &tgBot.SendMessageParams{
					ChatID: update.CallbackQuery.Message.Chat.ID,
					Text:   pleaseWaitMessage,
				})
				response = handlers.HandleTrainNumberCommand(ctx, trainNumber, date, -1)
				if err == nil {
					response.ProgressMessageToEditId = message.ID
				}
				handlers.SetChatFlow(chatFlow, handlers.InitialFlowType, handlers.InitialFlowType, "")

			case handlers.TrainInfoChooseGroupCallbackQuery:
				dateInt, _ := strconv.ParseInt(splitted[2], 10, 64)
				date := time.Unix(dateInt, 0)
				groupIndex, _ := strconv.ParseInt(splitted[3], 10, 31)
				log.Printf("%s, %v, %d", update.CallbackQuery.Data, splitted, groupIndex)
				originalResponse := handlers.HandleTrainNumberCommand(ctx, splitted[1], date, int(groupIndex))
				response = &handlers.HandlerResponse{
					MessageEdits: []*tgBot.EditMessageTextParams{
						{
							Text:                  originalResponse.Message.Text,
							ParseMode:             originalResponse.Message.ParseMode,
							Entities:              originalResponse.Message.Entities,
							DisableWebPagePreview: originalResponse.Message.DisableWebPagePreview,
							ReplyMarkup:           originalResponse.Message.ReplyMarkup,
						},
					},
				}
			}
		}
	}
}

func handleFindTrainStages(ctx context.Context, b *tgBot.Bot, update *models.Update, subs *subscriptions.Subscriptions) *handlers.HandlerResponse {
	log.Println("DEBUG: handleFindTrainStages")
	var response *handlers.HandlerResponse

	var chatId int64
	if update.Message != nil {
		chatId = update.Message.Chat.ID
	}
	if update.CallbackQuery != nil {
		chatId = update.CallbackQuery.Message.Chat.ID
	}
	chatFlow := handlers.GetChatFlow(chatId)
	switch chatFlow.Type {
	case handlers.InitialFlowType:
		// Only command is possible here
		commandParamsString := strings.TrimPrefix(update.Message.Text, trainInfoCommand)
		commandParamsString = strings.TrimSpace(commandParamsString)
		commandParams := strings.Split(commandParamsString, " ")
		if len(commandParams) > 1 {
			message, err := b.SendMessage(ctx, &tgBot.SendMessageParams{
				ChatID: update.Message.Chat.ID,
				Text:   pleaseWaitMessage,
			})
			trainNumber := commandParams[0]
			date := time.Now()
			groupIndex := -1

			if len(commandParams) > 1 {
				date, _ = time.Parse(time.RFC3339, commandParams[1])
			}
			if len(commandParams) > 2 {
				groupIndex, _ = strconv.Atoi(commandParams[2])
			}

			response = handlers.HandleTrainNumberCommand(ctx, trainNumber, date, groupIndex)
			if err == nil {
				response.ProgressMessageToEditId = message.ID
			}
		} else if len(commandParams) > 0 && len(commandParams[0]) != 0 {
			// Got only train number
			trainNumber := commandParams[0]
			response = getTrainInfoChooseDateResponse(trainNumber)
			handlers.SetChatFlow(chatFlow, handlers.TrainInfoFlowType, handlers.WaitingForDateStage, trainNumber)
		} else {
			response = &handlers.HandlerResponse{
				Message: &tgBot.SendMessageParams{
					Text: waitingForTrainNumberMessage,
				},
			}
			handlers.SetChatFlow(chatFlow, handlers.TrainInfoFlowType, handlers.WaitingForTrainNumberStage, "")
		}
	case handlers.TrainInfoFlowType:
		switch chatFlow.Stage {
		case handlers.WaitingForTrainNumberStage:
			trainNumber := update.Message.Text
			response = getTrainInfoChooseDateResponse(trainNumber)
			handlers.SetChatFlow(chatFlow, handlers.TrainInfoFlowType, handlers.WaitingForDateStage, trainNumber)
		case handlers.WaitingForDateStage:
			date, err := utils.ParseDate(update.Message.Text)
			if err != nil {
				response = &handlers.HandlerResponse{
					Message: &tgBot.SendMessageParams{
						Text: invalidDateMessage,
					},
				}
			} else {
				message, err := b.SendMessage(ctx, &tgBot.SendMessageParams{
					ChatID: update.Message.Chat.ID,
					Text:   pleaseWaitMessage,
				})
				response = handlers.HandleTrainNumberCommand(ctx, chatFlow.Extra, date, -1)
				if err == nil {
					response.ProgressMessageToEditId = message.ID
				}
				handlers.SetChatFlow(chatFlow, handlers.InitialFlowType, handlers.InitialFlowType, "")
			}
		}
	}
	return response
}

func getTrainInfoChooseDateResponse(trainNumber string) *handlers.HandlerResponse {
	replyButtons := make([][]models.InlineKeyboardButton, 0, 4)
	replyButtons = append(replyButtons, []models.InlineKeyboardButton{
		{
			Text:         fmt.Sprintf("Yesterday (%s)", time.Now().Add(time.Hour*-24).In(utils.Location).Format("02.01.2006")),
			CallbackData: fmt.Sprintf(handlers.TrainInfoChooseDateCallbackQuery+"\x1b%s\x1b%d", trainNumber, time.Now().Add(time.Hour*-24).Unix()),
		}, {
			Text:         fmt.Sprintf("Today (%s)", time.Now().In(utils.Location).Format("02.01.2006")),
			CallbackData: fmt.Sprintf(handlers.TrainInfoChooseDateCallbackQuery+"\x1b%s\x1b%d", trainNumber, time.Now().Unix()),
		},
	})
	for i := 1; i < 4; i++ {
		arr := make([]models.InlineKeyboardButton, 0, 7)
		for j := 0; j < 7; j++ {
			ts := time.Now().Add(time.Hour * time.Duration(24*(j+(i-1)*7+1))).In(utils.Location)
			arr = append(arr, models.InlineKeyboardButton{
				Text:         ts.Format("02.01"),
				CallbackData: fmt.Sprintf(handlers.TrainInfoChooseDateCallbackQuery+"\x1b%s\x1b%d", trainNumber, ts.Unix()),
			})
		}
		replyButtons = append(replyButtons, arr)
	}
	return &handlers.HandlerResponse{
		Message: &tgBot.SendMessageParams{
			Text: chooseDateMessage,
			ReplyMarkup: models.InlineKeyboardMarkup{
				InlineKeyboard: replyButtons,
			},
		},
	}
}
