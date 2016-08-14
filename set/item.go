package set

import "time"

// Interface for an item storeable in the set
type Item interface {
	Key() string
	Value() interface{}
}

type StringItem string

func (item StringItem) Key() string {
	return string(item)
}

func (item StringItem) Value() interface{} {
	return string(item)
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
