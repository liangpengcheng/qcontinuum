package db

var (

	// REDISCMD 当系统连接的是redis server的时候，就启用这个下表的数组作为命令 redisCmd
	REDISCMD = 0
	// SSDBCMD 同上处理ssdb的链接
	SSDBCMD = 1
	// USECMD 使用什么数据库，默认是redis
	USECMD = REDISCMD
)
var (
	cGET             = 0
	cSET             = 1
	cSETEX           = 2
	cDEL             = 3
	cINCRBY          = 4
	cDECRBY          = 5
	cKEYS            = 6
	cGETSET          = 7
	cSETNX           = 8
	cEXISTS          = 9
	cTTL             = 10
	cEXPIRE          = 11
	cGETBIT          = 12
	cSETBIT          = 13
	cSTRLEN          = 14
	cGETRANGE        = 15
	cHCLEAR          = 16
	cHGET            = 17
	cHSET            = 18
	cHDEL            = 19
	cHINCRBY         = 20
	cHDECRBY         = 21
	cHKEYS           = 22
	cHVALS           = 23
	cHMGET           = 24
	cHMSET           = 25
	cHLEN            = 26
	cHEXISTS         = 27
	cHLIST           = 28
	cZCLEAR          = 29
	cZSCORE          = 30
	cZADD            = 31
	cZREM            = 32
	cZRANGE          = 33
	cZRRANGE         = 34
	cZSCAN           = 35
	cZRSCAN          = 36
	cZINCRBY         = 37
	cZDECRBY         = 38
	cZCOUNT          = 39
	cZSUM            = 40
	cZAVG            = 41
	cZCARD           = 42
	cZRANK           = 43
	cZREMRANGBYRANK  = 44
	cZREMRANGBYSCORE = 45
	cZLIST           = 46
	cLCLEAR          = 47
	cLSIZE           = 48
	cLLPUSH          = 49
	cLRPUSH          = 50
	cLLPOP           = 51
	cLRPOP           = 52
	cLRANGE          = 53
	cLGET            = 54
	cLSET            = 55
	cLKEYS           = 56
	cAUTH            = 57
	cSELECT          = 58
	cHGETALL         = 59
	cEXEC            = 60
)
var redisCmd = [][]string{
	{"get", "get"},                           //cGET
	{"set", "set"},                           //cSET
	{"setx", "setx"},                         //cSETEX
	{"del", "del"},                           //cDEL
	{"incrby", "incr"},                       //cINCRBY
	{"decrby", "decr"},                       //cDECRBY
	{"keys", "keys"},                         //cKEYS
	{"getset", "getset"},                     //cGETSET
	{"setnx", "setnx"},                       //CSETNX
	{"exists", "exists"},                     //cEXISTS
	{"ttl", "ttl"},                           //cTTL
	{"expire", "expire"},                     //cEXPIRE
	{"getbit", "getbit"},                     //cGETBIT
	{"setbit", "setbit"},                     //cSETBIT
	{"strlen", "strlen"},                     //cSTRLEN
	{"getrange", "getrange"},                 //cGETRANGE
	{"del", "hclear"},                        //cHCLEAR
	{"hget", "hget"},                         //cHGET
	{"hset", "hset"},                         //cHSET
	{"hdel", "hdel"},                         //cHDEL
	{"hincrby", "hincr"},                     //cHINCRBY
	{"hdecrby", "hdecr"},                     //cHDECRBY
	{"hkeys", "hkeys"},                       //cHKEYS
	{"hvals", "hscan"},                       //cHVALS
	{"hmget", "multi_hget"},                  //cHMGET
	{"hmset", "multi_hset"},                  //cHMSET
	{"hlen", "hszie"},                        //cHLEN
	{"hexists", "hexists"},                   //cHEXISTS
	{"keys", "hlist"},                        //cHLIST
	{"del", "zclear"},                        //cZCLEAR
	{"zscore", "zget"},                       //cZSCORE
	{"zadd", "zset"},                         //cZADD
	{"zrem", "zdel"},                         //cZREM
	{"zrange", "zrange"},                     //cZRANGE
	{"zrevrange", "zrrange"},                 //cZRRANGE
	{"zerangebyscore", "zscan"},              //cZSCAN
	{"zrevrangebyscore", "zrscan"},           //cZRSCAN
	{"zincrby", "zincr"},                     //cZINCRBY
	{"zdecrby", "zdecr"},                     //cZDECRBY
	{"zcount", "zcount"},                     //cZCOUNT
	{"zsum", "zsum"},                         //cZSUM
	{"zavg", "zavg"},                         //cZAVG
	{"zcard", "zsize"},                       //cZCARD
	{"zrank", "zrank"},                       //cZRANK
	{"zremrangebyrank", "zremrangebyrank"},   //cZREMRANGBYRANK
	{"zremrangebyscore", "zremrangebyscore"}, //cZREMRANGBYSCORE
	{"keys", "zlist"},                        //cZLIST
	{"del", "qclear"},                        //cLCLEAR
	{"llen", "qsize"},                        //cLSIZE
	{"lpush", "qpush_front"},                 //cLLPUSH
	{"rpush", "qpush_back"},                  // cLRPUSH
	{"lpop", "qpop_front"},                   //cLLPOP
	{"rpop", "qpop_back"},                    // cLRPOP
	{"lrange", "qslice"},                     //cLRANGE
	{"lget", "qget"},                         //cLGET
	{"lset", "qset"},                         // cLSET
	{"keys", "qlist"},                        //cLKEYS
	{"auth", "auth"},                         //cAUTH
	{"select", "not support"},                //cSELECT
	{"hgetall", "hgetall"},                   //cHGETALL
	{"exec", "exec"},                         //cEXEC
}

func getRCmd(cmd int) string {
	return redisCmd[cmd][USECMD]
}

// GetRCmd 获得命令
func GetRCmd(cmd int) string {
	return getRCmd(cmd)
}
