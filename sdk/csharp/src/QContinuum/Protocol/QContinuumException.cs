namespace QContinuum.Protocol;

/// <summary>SDK 内所有可分类错误的统一异常类型。</summary>
public class QContinuumException : Exception
{
    public QErrorKind Kind { get; }

    public QContinuumException(QErrorKind kind, string message)
        : base(message) => Kind = kind;

    public QContinuumException(QErrorKind kind, string message, Exception inner)
        : base(message, inner) => Kind = kind;
}

/// <summary>未连接时调用 SendAsync 抛出的异常。</summary>
public sealed class QNotConnectedException : QContinuumException
{
    public QNotConnectedException()
        : base(QErrorKind.SendFailed, "Client is not connected.") { }
}
