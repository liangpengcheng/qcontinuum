using Google.Protobuf;

namespace QContinuum.Protocol;

/// <summary>
/// 解包后的业务消息：消息号 + protobuf 二进制体。
/// 与 Go 端 network.Message 对应（Head.ID / Body）。
/// </summary>
public sealed class QMessage
{
    /// <summary>消息号，对应 network.MessageHead.ID。</summary>
    public int Id { get; }

    /// <summary>protobuf 序列化后的消息体（已拷贝，调用方可安全持有）。</summary>
    public byte[] Body { get; }

    public QMessage(int id, byte[] body)
    {
        Id = id;
        Body = body;
    }

    /// <summary>把 Body 反序列化为 protobuf 消息。</summary>
    public T Unpack<T>() where T : IMessage<T>, new()
    {
        var msg = new T();
        msg.MergeFrom(Body);
        return msg;
    }
}
