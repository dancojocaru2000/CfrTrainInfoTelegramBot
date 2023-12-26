package subscriptions

import (
	"context"
	"dcdev.ro/CfrTrainInfoTelegramBot/pkg/handlers"
	"fmt"
	"github.com/go-telegram/bot"
	"log"
	"sync"
	"time"

	"dcdev.ro/CfrTrainInfoTelegramBot/pkg/database"
	"gorm.io/gorm"
)

type SubData struct {
	gorm.Model
	ChatId      int64
	MessageId   int
	TrainNumber string
	Date        time.Time
	GroupIndex  int
}

type Subscriptions struct {
	mutex sync.RWMutex
	data  map[int64][]SubData
	tgBot *bot.Bot
}

func LoadSubscriptions(tgBot *bot.Bot) (*Subscriptions, error) {
	subs := make([]SubData, 0)
	_, err := database.ReadDB(func(db *gorm.DB) (*gorm.DB, error) {
		result := db.Find(&subs)
		return result, result.Error
	})
	result := map[int64][]SubData{}
	for _, sub := range subs {
		result[sub.ChatId] = append(result[sub.ChatId], sub)
	}
	return &Subscriptions{
		mutex: sync.RWMutex{},
		data:  result,
		tgBot: tgBot,
	}, err
}

func (sub *Subscriptions) Replace(chatId int64, data []SubData) error {
	// Only allow replacing if all records use same chatId
	for _, d := range data {
		if d.ChatId != chatId {
			return fmt.Errorf("data contains item whose ChatId (%d) doesn't match chatId (%d)", d.ChatId, chatId)
		}
	}
	sub.mutex.Lock()
	defer sub.mutex.Unlock()
	sub.data[chatId] = data
	_, err := database.WriteDB(func(db *gorm.DB) (*gorm.DB, error) {
		db.Delete(&SubData{}, "chat_id = ?", chatId)
		db.Create(&data)
		return db, db.Error
	})
	return err
}

func (sub *Subscriptions) InsertSubscription(data SubData) error {
	sub.mutex.Lock()
	defer sub.mutex.Unlock()
	datas := sub.data[data.ChatId]
	datas = append(datas, data)
	sub.data[data.ChatId] = datas
	_, err := database.WriteDB(func(db *gorm.DB) (*gorm.DB, error) {
		db.Create(&data)
		return db, db.Error
	})
	return err
}

func (sub *Subscriptions) DeleteChat(chatId int64) error {
	sub.mutex.Lock()
	defer sub.mutex.Unlock()
	delete(sub.data, chatId)
	_, err := database.WriteDB(func(db *gorm.DB) (*gorm.DB, error) {
		db.Delete(&SubData{}, "chat_id = ?", chatId)
		return db, db.Error
	})
	return err
}

func (sub *Subscriptions) DeleteSubscription(chatId int64, messageId int) (*SubData, error) {
	sub.mutex.Lock()
	defer sub.mutex.Unlock()
	datas := sub.data[chatId]
	deleteIndex := -1
	for i := range datas {
		if datas[i].MessageId == messageId {
			deleteIndex = i
			break
		}
	}
	var result SubData
	if deleteIndex != -1 {
		result = datas[deleteIndex]
		datas[deleteIndex] = datas[len(datas)-1]
		datas = datas[:len(datas)-1]

		_, err := database.WriteDB(func(db *gorm.DB) (*gorm.DB, error) {
			db.Delete(&result)
			return db, db.Error
		})
		if err != nil {
			return nil, err
		}
	} else {
		return nil, fmt.Errorf("subscription chatId %d messageId %d not found", chatId, messageId)
	}
	if len(datas) == 0 {
		delete(sub.data, chatId)
	} else {
		sub.data[chatId] = datas
	}
	return &result, nil
}

func (sub *Subscriptions) CheckSubscriptions(ctx context.Context) {
	ticker := time.NewTicker(time.Second * 90)

	sub.executeChecks(ctx)
	for {
		select {
		case <-ticker.C:
			sub.executeChecks(ctx)
		case <-ctx.Done():
			return
		}
	}
}

type workerData struct {
	tgBot *bot.Bot
	data  SubData
}

type unsubscribe struct {
	chatId    int64
	messageId int
}

type workerResponseData struct {
	unsubscribe *unsubscribe
}

func (sub *Subscriptions) executeChecks(ctx context.Context) {
	sub.mutex.RLock()

	// Only allow 8 concurrent requests
	// TODO: Make configurable instead of hardcoded
	workerCount := 8
	workerChan := make(chan workerData, workerCount)
	responseChan := make(chan *workerResponseData, workerCount)
	defer close(responseChan)
	for i := 0; i < workerCount; i++ {
		go checkWorker(ctx, workerChan, responseChan)
	}

	go func() {
		for _, datas := range sub.data {
			for i := range datas {
				workerChan <- workerData{
					tgBot: sub.tgBot,
					data:  datas[i],
				}
			}
		}
		close(workerChan)
	}()

	responses := make([]*workerResponseData, 0, len(sub.data))

	for _, datas := range sub.data {
		for range datas {
			if resp := <-responseChan; resp != nil && resp.unsubscribe != nil {
				responses = append(responses, resp)
			}
		}
	}

	sub.mutex.RUnlock()

	for i := range responses {
		if responses[i].unsubscribe != nil {
			// Ignore error since this is optional optimisation
			deletedSub, err := sub.DeleteSubscription(responses[i].unsubscribe.chatId, responses[i].unsubscribe.messageId)
			if err == nil && deletedSub != nil {
				_, _ = sub.tgBot.EditMessageReplyMarkup(ctx, &bot.EditMessageReplyMarkupParams{
					ChatID:      responses[i].unsubscribe.chatId,
					MessageID:   responses[i].unsubscribe.messageId,
					ReplyMarkup: handlers.GetTrainNumberCommandResponseButtons(deletedSub.TrainNumber, deletedSub.Date, deletedSub.GroupIndex, handlers.TrainInfoResponseButtonExcludeSub),
				})
			}
		}
	}
}

func checkWorker(ctx context.Context, workerChan <-chan workerData, responseChan chan<- *workerResponseData) {
	for wData := range workerChan {
		func() {
			var response *workerResponseData
			defer func() {
				responseChan <- response
			}()
			data := wData.data
			log.Printf("DEBUG: Timer tick, update for chat %d, train %s, date %s, group %d", data.ChatId, data.TrainNumber, data.Date.Format("2006-01-02"), data.GroupIndex)

			resp, ok := handlers.HandleTrainNumberCommand(ctx, data.TrainNumber, data.Date, data.GroupIndex, true)

			if !ok || resp == nil || resp.Message == nil {
				// Silently discard update errors
				log.Printf("DEBUG: Error when updating chat %d, train %s, date %s, group %d", data.ChatId, data.TrainNumber, data.Date.Format("2006-01-02"), data.GroupIndex)
				if resp != nil && resp.ShouldUnsubscribe {
					response = &workerResponseData{
						unsubscribe: &unsubscribe{
							chatId:    data.ChatId,
							messageId: data.MessageId,
						},
					}
				}
				return
			}

			_, _ = wData.tgBot.EditMessageText(ctx, &bot.EditMessageTextParams{
				ChatID:                data.ChatId,
				MessageID:             data.MessageId,
				Text:                  resp.Message.Text,
				ParseMode:             resp.Message.ParseMode,
				Entities:              resp.Message.Entities,
				DisableWebPagePreview: resp.Message.DisableWebPagePreview,
				ReplyMarkup:           resp.Message.ReplyMarkup,
			})

			response = &workerResponseData{}
			if resp.ShouldUnsubscribe {
				response.unsubscribe = &unsubscribe{
					chatId:    data.ChatId,
					messageId: data.MessageId,
				}
			}
		}()
	}
}
