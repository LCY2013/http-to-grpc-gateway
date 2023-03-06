package async

import (
	"fmt"
	"runtime/debug"
)

func GO(f func()) {
	go func() {
		defer func() {
			if err := recover(); err != nil {
				fmt.Println(string(debug.Stack()))
			}
		}()

		f()
	}()
}
