package network

import (
	"runtime/debug"
	"time"

	"github.com/liangpengcheng/qcontinuum/base"
)

// MsgCallback 消息处理函数
type MsgCallback func(msg *Message)

// EventCallback 时间处理
type EventCallback func(event *Event)

// ProcFunction 回掉的程序
type ProcFunction func()

// Message 消息链
type Message struct {
	Peer *ClientPeer
	Head MessageHead
	Body []byte
}

// UnmarshalBody decodes Body into v using the provided codec or the package default.
// This is an additive helper that allows formats other than Protobuf without breaking
// existing users who directly decode from Body.
func (m *Message) UnmarshalBody(v interface{}, codec ...Codec) error {
	var c Codec
	if len(codec) > 0 && codec[0] != nil {
		c = codec[0]
	} else {
		c = DefaultCodec()
	}
	return c.Unmarshal(m.Body, v)
}

// Event 自定义事件
type Event struct {
	ID    int32       //eventid
	Param string      //param
	Peer  *ClientPeer //事件中的peer,可以为nil
}

var (
	// ExitEvent 退出
	ExitEvent int32 = 1
	// AddEvent 加入玩家
	AddEvent int32 = 2
	// RemoveEvent 删除玩家
	RemoveEvent int32 = 3
)

// Processor 消息处理器
type Processor struct {
	MessageChan chan *Message
	//SendChan       chan *Message
	EventChan        chan *Event
	FuncChan         chan ProcFunction
	CallbackMap      map[int32]MsgCallback
	UnHandledHandler MsgCallback //未注册的消息处理
	EventCallback    map[int32]EventCallback
	updateCallback   ProcFunction
	// 更新时间
	loopTime time.Duration
	// ImmediateMode 立即回调消息，如果想要线程安全，必须设置为false，默认为false
	ImmediateMode bool
}

// NewProcessor 新建处理器，包含初始化操作
func NewProcessor() *Processor {
	return NewProcessorWithLoopTime(24 * time.Hour)
}

// NewProcessorWithLoopTime 指定定时器
func NewProcessorWithLoopTime(time time.Duration) *Processor {
	p := &Processor{
		MessageChan: make(chan *Message, 1024),
		//SendChan:      make(chan *Message, 1024),
		EventChan:     make(chan *Event, 1024),
		FuncChan:      make(chan ProcFunction, 64),
		EventCallback: make(map[int32]EventCallback),
		CallbackMap:   make(map[int32]MsgCallback),
		loopTime:      time,
		ImmediateMode: false,
	}
	return p
}

// AddCallback 设置回调
func (p *Processor) addCallback(id int32, callback MsgCallback) {
	p.CallbackMap[id] = callback
}

// AddCallback 设置回调
func (p *Processor) AddCallback(id int32, callback MsgCallback) {
	if id != 0 {
		p.addCallback(id, callback)
	}
}

// SetUpdate 设置更新时间，以及更新函数,第一个参数，时间的设定，只在调用StartProcess之前调用起作用
func (p *Processor) SetUpdate(uptime time.Duration, upcall ProcFunction) {
	p.loopTime = uptime
	p.updateCallback = upcall
}

// RemoveCallback 删除回调
func (p *Processor) RemoveCallback(id int32) {
	delete(p.CallbackMap, id)
}

// AddEventCallback 事件处理函数注册
func (p *Processor) AddEventCallback(id int32, callback EventCallback) {
	p.EventCallback[id] = callback
}

// RemoveEventCallback 删除事件处理回调
func (p *Processor) RemoveEventCallback(id int32) {
	delete(p.EventCallback, id)
}

/*
func (p *Processor) send() {
	for {
		select {
		case msg := <-p.SendChan:
			if msg.Peer.Connection != nil {
				msg.Peer.Connection.Write(msg.Body)
			}
		}
	}
}
*/
// StartProcess 开始处理信息
// 只有调用了这个接口，处理器才会处理实际的信息，以及实际发送消息
func (p *Processor) StartProcess() {
	base.Zap().Sugar().Infof("processor is starting")
	tick := time.Tick(p.loopTime)

	for {
		// 使用safeProcessOnce处理单次循环，确保panic不会导致退出
		shouldExit := p.safeProcessOnce(tick)
		if shouldExit {
			return
		}
	}
}

// safeProcessOnce 安全地处理一次事件循环，捕获panic并返回是否应该退出
func (p *Processor) safeProcessOnce(tick <-chan time.Time) (shouldExit bool) {
	defer func() {
		if r := recover(); r != nil {
			base.Zap().Sugar().Errorf("processor recovered from panic: %v", r)
			base.Zap().Sugar().Errorf("stack trace:\n%s", debug.Stack())
			// panic后不退出，继续处理下一个事件
			shouldExit = false
		}
	}()

	select {
	case msg := <-p.MessageChan:
		p.handleMessage(msg)
	case event := <-p.EventChan:
		if event.ID == ExitEvent {
			base.Zap().Sugar().Infof("Processor exit: %s", event.Param)
			return true
		}
		p.handleEvent(event)
	case f := <-p.FuncChan:
		p.handleFunc(f)
	case <-tick:
		if p.updateCallback != nil {
			p.updateCallback()
		}
	}

	return false
}

// handleMessage 安全地处理消息
func (p *Processor) handleMessage(msg *Message) {
	if cb, ok := p.CallbackMap[msg.Head.ID]; ok {
		cb(msg)
	} else if p.UnHandledHandler != nil {
		p.UnHandledHandler(msg)
	} else {
		base.Zap().Sugar().Warnf("can't find callback(%d)", msg.Head.ID)
	}
}

// handleEvent 安全地处理事件
func (p *Processor) handleEvent(event *Event) {
	if cb, ok := p.EventCallback[event.ID]; ok {
		cb(event)
	}
}

// handleFunc 安全地处理函数调用
func (p *Processor) handleFunc(f ProcFunction) {
	f()
}
