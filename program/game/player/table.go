package player

import (
	"sync"
	"chessSever/program/game"
	"strconv"
	"fmt"
	"errors"
	"time"
	"math/rand"
	"github.com/gorilla/websocket"
	"chessSever/program/game/games/doudizhu"
	"encoding/json"
	"chessSever/program/game/games"
	"chessSever/program/game/poker"
	"chessSever/program/game/msg"
)

/*
	定义游戏桌对象
*/
type Table struct {
	Key             string               //桌子key,用于从room索引中查找桌子
	Players         []*Player            //玩家数组
	Game            game.IGame          //该桌玩的游戏
	sync.RWMutex                         //操作playNum以及player时加锁
 	IsPlaying       bool                 //是否正在游戏中
	LoardIndex      int                  //当前地主的Index
	CurrLoardScore  int                  //当前地主分数
	CalledLoardNum  int                  //叫过地主的人数
	CurrPlayerIndex int                  //当前叫地主或者出牌人的index
	LastCards       *game.LastCardsType //最后的出牌结构
	OutCardIndexs   []int                //出完牌的用户index
}
//创建桌子
func newTable(player *Player, gameID int) *Table {

	currGame := games.GetGame(gameID)
	table := Table{
		Game: currGame,
		Key:  "table" + strconv.Itoa(time.Now().Nanosecond()),//桌子的key要保证唯一且好找，所以用时间戳，
		Players:make([]*Player,currGame.GetPlayerNum()),
		IsPlaying:false,
	}
	fmt.Println("创建新桌子"+"table" + strconv.Itoa(player.User.Id))
	//桌子加入房间
	table.joinRoom()
	//将创建者加入桌子
	table.addPlayer(player)
	return &table
}
//加入房间
func (t *Table) joinRoom() {
	GetRoom().addTable(t.Key, t)
}
//销毁桌子
func (t *Table) destory() {
	t.RLock()
	if len(t.Players) >= 0 {
		for _, p := range t.Players {
			p.LeaveTable()
		}
	}
	GetRoom().removeTable(t.Key)
	fmt.Println("桌子"+t.Key+"销毁")
	t.RUnlock()
}
//增加玩家
func (t *Table) addPlayer(player *Player) error {
	t.Lock()
	if(len(t.Players) >= t.Game.GetPlayerNum()){
		for i,p := range t.Players{
			if p == nil{
				t.Players[i] = player
				fmt.Println(t.Key+"有新玩家加入")
				t.Unlock()
				t.BroadCastMsg(player,msg.MSG_TYPE_OF_JOIN_TABLE,"玩家加入桌子")
				return nil
			}else{
				if(i == len(t.Players)){
					t.Unlock()
					return errors.New("该桌玩家已满")
				}
			}
		}
		t.Unlock()
		return errors.New("该桌玩家已满")
	}else{
		t.Players = append(t.Players,player)
		fmt.Println(t.Key+"有新玩家加入")
		t.Unlock()
		t.BroadCastMsg(player,msg.MSG_TYPE_OF_JOIN_TABLE,"玩家加入桌子")
		return nil
	}
}
//移除玩家
func (t *Table) removePlayer(player *Player) {
	t.Lock()
	for i, p := range t.Players {
		if p == player {
			t.Players[i] = nil
			break
		}
	}
	t.Unlock()
	t.BroadCastMsg(player,msg.MSG_TYPE_OF_LEAVE_TABLE,"玩家离开桌子")
	fmt.Println("桌子"+t.Key+"移除玩家"+strconv.Itoa(player.User.Id))

}

func (t *Table) userReady(){
	t.Lock()
	userAllReady := false
	for _,p := range t.Players{
		if p != nil && p.IsReady{
			userAllReady = true
		}else{
			userAllReady = false
		}
	}
	//用户都准备好了，则发牌
	if userAllReady {
		fmt.Println(t.Key+"的玩家都准备好了")
		t.IsPlaying = true
		t.Unlock()
		t.Game.DealCards()
		t.dealCards()
	}else{
		t.Unlock()
	}
}

func (t *Table) dealCards(){
	fmt.Println("开始发牌")
	for i,player := range t.Players{
		player.PokerCards = t.Game.GetPlayerCards(i)
		sendPlayerCards(player)
	}
	t.callLoard()
}

func (t *Table) callLoard(){
	rand.Seed(time.Now().Unix())
	currUserIndex := rand.Int31n(int32(t.Game.GetPlayerNum()-1))
	t.nextCallLoard(int(currUserIndex))
}

func (t *Table) userCallScore(player *Player,score int){
	t.Lock()
	t.CalledLoardNum++
	var i int
	var p *Player
	for i,p = range t.Players{
		if p == player {
			break
		}
	}

	if score == 3{
		t.CurrLoardScore = score
		t.LoardIndex = i
		t.Unlock()
		t.callLoardEnd()
	}else{
		if t.CalledLoardNum >= t.Game.GetPlayerNum() {
			t.Unlock()
			t.callLoardEnd()
		}else{
			if score > t.CurrLoardScore{
				t.CurrLoardScore = score
				t.LoardIndex = i
				t.Unlock()
				t.nextCallLoard(-1)
			}else{
				t.Unlock()
				t.nextCallLoard(-1)
			}
		}
	}
	t.BroadCastMsg(player,msg.MSG_TYPE_OF_CALL_SCORE,"用户叫地主")
}

func (t *Table) callLoardEnd(){
	t.Lock()
	t.CurrPlayerIndex = 0
	t.CalledLoardNum = 0
	t.Unlock()
	fmt.Println("叫地主结束"+strconv.Itoa(t.LoardIndex)+"成为地主")
	currPlayer := t.Players[t.LoardIndex]

	for _,card := range t.Game.GetBottomCards(){
		currPlayer.PokerCards = append(currPlayer.PokerCards,card)
	}
	poker.CommonSort(t.Players[t.LoardIndex].PokerCards)
	sendPlayerCards(t.Players[t.LoardIndex])

	t.BroadCastMsg(t.Players[t.LoardIndex],msg.MSG_TYPE_OF_SEND_BOTTOM_CARDS,"发放底牌")
	fmt.Println("底牌发送完毕，开始游戏")
	t.play(nil)
}

func (t *Table) nextCallLoard(index int){

	var player *Player
	if index >= 0{
		t.Lock()
		t.CurrPlayerIndex = index
		player = t.Players[t.CurrPlayerIndex]
		t.Unlock()
	}else{
		player = t.GetNextLoard()
	}

	SendMsgToPlayer(player,msg.MSG_TYPE_OF_CALL_SCORE,"叫地主")
	//限制叫地主时间
	go func(){
		time.Sleep(time.Second*10)
		player.RLock()
		if !player.CalledScore{
			player.RUnlock()
			SendMsgToPlayer(player,msg.MSG_TYPE_OF_CALL_SCORE_TIME_OUT,"叫地主")
			player.callScore(0)
		}else{
			player.RUnlock()
		}
	}()
}
/*
	player下一个出牌的玩家
	isPass当前玩家是否是过牌
 */
func (t *Table) play(player *Player){
	if player == nil{
		t.CurrPlayerIndex = t.LoardIndex
		player = t.Players[t.LoardIndex]
	}
	SendMsgToPlayer(player,msg.MSG_TYPE_OF_PLAY_CARD,"玩家出牌")
}

func (t *Table) userPlayCard(p *Player,cardIndexs []int){
	//符合出牌规则才允许出牌
	if t.GetCurrPlayerIndex(p) != t.CurrPlayerIndex{
		p.playCardError("还没到您的出牌次序")
		return
	}

	cards := []*poker.PokerCard{}
	p.RLock()
	for _,index := range cardIndexs{
		//判断是否是之前出过的牌
		if p.PlayedCardIndexs != nil {
			for _,playedIndex := range p.PlayedCardIndexs{
				if index == playedIndex {
					p.playCardError("出牌中包含已出的牌")
					p.RUnlock()
					return
				}
			}
		}
		cards = append(cards,p.PokerCards[index])
	}
	p.RUnlock()

	lastCards,err := t.Game.MatchRoles(t.GetCurrPlayerIndex(p),cards)
	if err == nil{

		if  t.LastCards == nil || lastCards.PlayerIndex == t.LastCards.PlayerIndex ||
			(lastCards.CardsType == t.LastCards.CardsType &&
			lastCards.CardMinAndMax["min"] > t.LastCards.CardMinAndMax["min"] &&
			lastCards.CardMinAndMax["max"] > t.LastCards.CardMinAndMax["min"]){

				if lastCards.PlayerCardIndexs == nil{
					lastCards.PlayerCardIndexs = []int{}
				}

				if p.PlayedCardIndexs == nil{
					p.PlayedCardIndexs = []int{}
				}

				for _,index := range cardIndexs{
					lastCards.PlayerCardIndexs = append(lastCards.PlayerCardIndexs,index)
					p.PlayedCardIndexs = append(p.PlayedCardIndexs,index)
				}

				isBomb := false
				t.Lock()
				t.LastCards = lastCards
				if t.LastCards.CardsType == doudizhu.POKERS_TYPE_COMMON_BOMB ||
					t.LastCards.CardsType == doudizhu.POKERS_TYPE_JOKER_BOMB{
					isBomb = true
					t.CurrLoardScore = t.CurrLoardScore*2
				}
				t.Unlock()
				if(isBomb){
					t.BroadCastMsg(p,msg.MSG_TYPE_OF_SCORE_CHANGE,"分数加倍")
				}
				//出牌成功，给前端提示
				p.playCardSuccess()

				t.BroadCastMsg(p,msg.MSG_TYPE_OF_PLAY_CARD,"玩家出牌")
				//玩家的牌全部出完了
				if len(p.PlayedCardIndexs) == len(p.PokerCards) {
					if t.OutCardIndexs == nil{
						t.OutCardIndexs = []int{}
					}

					currIndex := t.GetCurrPlayerIndex(p)
					t.OutCardIndexs = append(t.OutCardIndexs,currIndex)

					if currIndex == t.LoardIndex{
						t.gameOver()
						return
					}else{
						if len(t.OutCardIndexs) == 2{
							t.gameOver()
							return
						}
					}
				}
				//下一个玩家出牌
				t.play(t.GetNextPlayer())

		}else{
			p.playCardError("出牌必须大于上一家")
		}

	}else{
		p.playCardError(err.Error())
	}
}

func (t *Table) gameOver(){
	if len(t.OutCardIndexs) == 1 {
		t.BroadCastMsg(nil,msg.MSG_TYPE_OF_GAME_OVER,"游戏结束,地主胜利")
	}else{
		if t.OutCardIndexs[1] == t.LoardIndex{
			t.BroadCastMsg(nil,msg.MSG_TYPE_OF_GAME_OVER,"游戏结束,地主胜利")
		}else{
			t.BroadCastMsg(nil,msg.MSG_TYPE_OF_GAME_OVER,"游戏结束,农民胜利")
		}
	}
}

func (t *Table) userPassCard(player *Player){
	//之前出牌是当前玩家则不能过牌，第一个出牌玩家也不能过牌
	if t.LastCards != nil && t.GetCurrPlayerIndex(player) != t.LastCards.PlayerIndex{
		player.playCardSuccess()
		t.BroadCastMsg(player,msg.MSG_TYPE_OF_PASS,"用户过牌")
		t.play(t.GetNextPlayer())
	}else{
		player.playCardError("第一个出牌的玩家不能过牌")
	}
}
func (t *Table) GetNextPlayer() *Player{
	t.Lock()
	defer t.Unlock()
	if(t.CurrPlayerIndex >= t.Game.GetPlayerNum()-1){
		t.CurrPlayerIndex = 0
	}else{
		t.CurrPlayerIndex++
	}

	return t.Players[t.CurrPlayerIndex]
}

func (t *Table) GetNextLoard() *Player{
	t.Lock()
	defer t.Unlock()
	if(t.CurrPlayerIndex >= t.Game.GetPlayerNum()-1){
		t.CurrPlayerIndex = 0
	}else{
		t.CurrPlayerIndex++
	}

	return t.Players[t.CurrPlayerIndex]
}

func (t *Table) GetCurrPlayerIndex(player *Player) int {
	t.RLock()
	defer t.RUnlock()
	for i,p := range t.Players{
		if(p == player){
			return i
		}
	}
	return -1
}
/*
	准备
	取消准备
    离开房间
	进入房间
	叫地主
	出牌
	过牌
	分数变化
	结算
 */
func (t *Table) BroadCastMsg(player *Player,msgType int,hints string){

	newMsg := msg.NewBraodCastMsg()
	newMsg.SubMsgType = msgType

	t.RLock()
	defer t.RUnlock()

	if player != nil{
		newMsg.PlayerId = player.User.Id
		for i,p := range t.Players{
			if p != nil{
				newMsg.PlayerIndexIdDic["id"+strconv.Itoa(p.User.Id)] = i
			}
		}
	}

	switch msgType{
		case msg.MSG_TYPE_OF_READY:
			newMsg.Msg = strconv.Itoa(player.User.Id)+"已准备"
		case msg.MSG_TYPE_OF_UN_READY:
			newMsg.Msg = strconv.Itoa(player.User.Id)+"取消准备"
		case msg.MSG_TYPE_OF_JOIN_TABLE:
			newMsg.Msg = strconv.Itoa(player.User.Id)+"加入游戏"
		case msg.MSG_TYPE_OF_LEAVE_TABLE:
			newMsg.Msg = strconv.Itoa(player.User.Id)+"离开游戏"
		case msg.MSG_TYPE_OF_PLAY_CARD:
			newMsg.Msg = strconv.Itoa(player.User.Id)+"出牌"
			for _,card := range t.LastCards.Cards{
				newMsg.Cards = append(newMsg.Cards,card)
			}
		case msg.MSG_TYPE_OF_PASS:
			newMsg.Msg = strconv.Itoa(player.User.Id)+"过牌"
		case msg.MSG_TYPE_OF_CALL_SCORE:
			newMsg.Msg = strconv.Itoa(player.User.Id)+"叫地主"
			newMsg.Score = player.CallScore
		case msg.MSG_TYPE_OF_SCORE_CHANGE:
			newMsg.Msg = "基础变动"
			newMsg.Score = t.CurrLoardScore
		case msg.MSG_TYPE_OF_SEND_BOTTOM_CARDS:
			newMsg.Msg = "发放底牌"
			newMsg.Cards = t.Game.GetBottomCards()
			newMsg.PlayerId = player.User.Id
		case msg.MSG_TYPE_OF_GAME_OVER:
			newMsg.Msg = "游戏结束，结算积分"
			newMsg.Score = t.CurrLoardScore
			for _,index := range t.OutCardIndexs{
				if index == t.LoardIndex{
					newMsg.SettleInfoDic["id"+strconv.Itoa(t.Players[index].User.Id)] = "+"+strconv.Itoa(t.CurrLoardScore*2)
				}else{
					newMsg.SettleInfoDic["id"+strconv.Itoa(t.Players[index].User.Id)] = "+"+strconv.Itoa(t.CurrLoardScore)
				}
			}

			for i,player := range t.Players{
				_,ok := newMsg.SettleInfoDic["id"+strconv.Itoa(player.User.Id)]
				if !ok{
					if i == t.LoardIndex{
						newMsg.SettleInfoDic["id"+strconv.Itoa(t.Players[i].User.Id)] = "-"+strconv.Itoa(t.CurrLoardScore*2)
					}else{
						newMsg.SettleInfoDic["id"+strconv.Itoa(t.Players[i].User.Id)] = "-"+strconv.Itoa(t.CurrLoardScore)
					}
				}
			}
	}

	msgJson,err := json.Marshal(newMsg)
	if err != nil {
		panic(err.Error())
	}

	for _,player := range t.Players{
		if player != nil{
			player.Conn.WriteMessage(websocket.TextMessage,msgJson)
		}
	}
}





