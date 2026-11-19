package sandbox

import "time"

func timeLimit(seconds int)time.Duration{return time.Duration(seconds)*time.Second}
