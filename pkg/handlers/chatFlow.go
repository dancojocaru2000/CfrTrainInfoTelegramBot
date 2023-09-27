package handlers

import (
	"log"

	"dcdev.ro/CfrTrainInfoTelegramBot/pkg/database"
	"gorm.io/gorm"
)

const (
	InitialFlowType     = "initial"
	TrainInfoFlowType   = "trainInfo"
	StationInfoFlowType = "stationInfo"
	RouteFlowType       = "route"

	WaitingForTrainNumberStage = "waitingForTrainNumber"
	WaitingForDateStage        = "waitingForDate"
)

type ChatFlow struct {
	gorm.Model
	ChatId int64
	Type   string
	Stage  string
	Extra  string
}

func GetChatFlow(chatId int64) *ChatFlow {
	chatFlow := &ChatFlow{}
	result, _ := database.ReadDB(func(db *gorm.DB) (*gorm.DB, error) {
		return db.First(chatFlow, "chat_id = ?", chatId), nil
	})
	if result.RowsAffected == 0 {
		log.Printf("DEBUG: Chat not found in DB: %d\n", chatId)
		chatFlow = &ChatFlow{
			ChatId: chatId,
			Type:   InitialFlowType,
		}
		_, _ = database.WriteDB(func(db *gorm.DB) (*gorm.DB, error) {
			return db.Create(chatFlow), nil
		})
	} else {
		log.Printf("DEBUG: Chat found in DB: %d, type %s, stage %s\n", chatId, chatFlow.Type, chatFlow.Stage)
	}
	return chatFlow
}

func SetChatFlow(chatFlow *ChatFlow, flowType string, stage string, extra string) {
	_, _ = database.WriteDB(func(db *gorm.DB) (*gorm.DB, error) {
		return db.Model(chatFlow).Updates(ChatFlow{
			Type:  flowType,
			Stage: stage,
			Extra: extra,
		}), nil
	})
	log.Printf("DEBUG: setChatFlow type %s, stage %s", flowType, stage)
}
