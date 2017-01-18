package network

// MsgCallback 消息处理函数
type MsgCallback func(msg Message)

// Message 消息链
type Message struct {
	Peer *ClientPeer
	Head MessageHead
	Body []byte
}

// Processor 消息处理器
type Processor struct {
	MessageChan chan Message
}

// NewProcessor 新建处理器，包含初始化操作
func NewProcessor() *Processor {
	p := &Processor{
		MessageChan: make(chan Message),
	}
	return p
}
