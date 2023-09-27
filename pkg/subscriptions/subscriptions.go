package subscriptions

import (
	"context"
	"fmt"
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
}

type Subscriptions struct {
	mutex sync.RWMutex
	data  map[int64][]SubData
}

func LoadSubscriptions() (*Subscriptions, error) {
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

func (sub *Subscriptions) InsertSubscription(chatId int64, data SubData) error {
	sub.mutex.Lock()
	defer sub.mutex.Unlock()
	datas := sub.data[chatId]
	datas = append(datas, data)
	sub.data[chatId] = datas
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
	var result *SubData
	if deleteIndex != -1 {
		result = &SubData{}
		*result = datas[deleteIndex]
		datas[deleteIndex] = datas[len(datas)-1]
		datas = datas[:len(datas)-1]

		_, err := database.WriteDB(func(db *gorm.DB) (*gorm.DB, error) {
			db.Delete(result)
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
	return result, nil
}

func (sub *Subscriptions) CheckSubscriptions(ctx context.Context) {
	ticker := time.NewTicker(time.Second * 90)

	for {
		select {
		case <-ticker.C:
			func() {
				sub.mutex.RLock()
				defer sub.mutex.RUnlock()

				for chatId, datas := range sub.data {
					// TODO: Check for updates
					for i := range datas {
						data := &datas[i]
						log.Printf("DEBUG: Timer tick, update for chat %d, train %s", chatId, data.TrainNumber)
					}
				}
			}()
		case <-ctx.Done():
			return
		}
	}
}
