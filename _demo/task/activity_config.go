package task

import (
	"sync"
)

var (
	setNameLocker         sync.Mutex //避免冲突
	namespaceLinkNameChar = "/"      // 命名之间的默认连接字符
	returnKeyPrefix       = ""
)

func ReturnKeyPrefix() string {
	return returnKeyPrefix
}
func WithReturnKeyPrefix(keyPrefix string) {
	if returnKeyPrefix == keyPrefix {
		return
	}
	setNameLocker.Lock()
	defer setNameLocker.Unlock()
	returnKeyPrefix = keyPrefix
}
func WithKeyLinkCharacter(char string) {
	if namespaceLinkNameChar == char {
		return
	}
	setNameLocker.Lock()
	defer setNameLocker.Unlock()
	namespaceLinkNameChar = char
}
