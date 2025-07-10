package controller

import "fmt"

// RequeueError 自定义错误类型，实现了 error 接口
type RequeueError struct {
	Reason string
}

// 实现 error 接口的 Error() 方法
func (e *RequeueError) Error() string {
	return fmt.Sprintf("Requeue needed: %s", e.Reason)
}

func PodNotCompletionError(reason string) error {
	return &RequeueError{Reason: reason}
}

// 判断错误是否是 PodNotCompletionError 类型
func IsPodNotCompletionError(err error) bool {
	_, ok := err.(*RequeueError)
	return ok
}
