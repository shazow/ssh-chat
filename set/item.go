package set

import "time"

// Interface for an item storeable in the set
type Item interface {
	Key() string
	Value() interface{}
}

type item struct {
	key   string
	value interface{}
}

func (item *item) Key() string {
	return item.key
}

func (item *item) Value() interface{} {
	return item.value
}

func Itemize(key string, value interface{}) Item {
	return &item{key, value}
}

type StringItem string

func (item StringItem) Key() string {
	return string(item)
}

func (item StringItem) Value() interface{} {
	return true
}

func Expire(item Item, d time.Duration) Item {
	return &ExpiringItem{
		Item: item,
		Time: time.Now().Add(d),
	}
}

type ExpiringItem struct {
	Item
	time.Time
}

func (item *ExpiringItem) Expired() bool {
	return time.Now().After(item.Time)
}

func (item *ExpiringItem) Value() interface{} {
	if item.Expired() {
		return nil
	}
	return item.Item.Value()
}
