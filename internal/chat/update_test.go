package chat

import "flag"

// -update 用来重新生成金标文件。收在一处，将来多几个金标测试也共用这一个开关。
var update = flag.Bool("update", false, "重新生成测试金标文件")
