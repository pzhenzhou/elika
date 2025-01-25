package respio

type TxCmdStateType string

const (
	TxCmdStateBegin TxCmdStateType = "begin"
	TxCmdStateEnd   TxCmdStateType = "end"
)

var (
	AuthCmd    = []byte("AUTH")
	MultiCmd   = []byte("multi")
	WatchCmd   = []byte("watch")
	ExecCmd    = []byte("exec")
	DiscardCmd = []byte("discard")
)

const (
	CRLF     = "\r\n"
	Nil      = "$-1\r\n"
	NilArray = "*-1\r\n"
)

const (
	RespStatus    = byte('+') // +<string>\r\n
	RespError     = byte('-') // -<string>\r\n
	RespString    = byte('$') // $<length>\r\n<bytes>\r\n
	RespInt       = byte(':') // :<number>\r\n
	RespNil       = byte('_') // _\r\n
	RespFloat     = byte(',') // ,<floating-point-number>\r\n (golang float)
	RespBool      = byte('#') // true: #t\r\n false: #f\r\n
	RespBlobError = byte('!') // !<length>\r\n<bytes>\r\n
	RespVerbatim  = byte('=') // =<length>\r\nFORMAT:<bytes>\r\n
	RespBigInt    = byte('(') // (<big number>\r\n
	RespArray     = byte('*') // *<len>\r\n... (same as resp2)
	RespMap       = byte('%') // %<len>\r\n(key)\r\n(value)\r\n... (golang map)
	RespSet       = byte('~') // ~<len>\r\n... (same as Array)
	RespAttr      = byte('|') // |<len>\r\n(key)\r\n(value)\r\n... + command reply
	RespPush      = byte('>') // ><len>\r\n... (same as Array)
)
