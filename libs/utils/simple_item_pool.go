package utils

type SimpleItemFactory interface {
	New() interface{}
}

type SimpleItemPool struct {
	items         []interface{}
	can_use_start int32
	can_use_num   int32
}

func (list *SimpleItemPool) Init(max_num int32, factory SimpleItemFactory) {
	list.items = make([]interface{}, max_num)
	for i := int32(0); i < max_num; i++ {
		list.items[i] = factory.New()
	}
	list.can_use_start = 0
	list.can_use_num = max_num
}

func (list *SimpleItemPool) GetFree() interface{} {
	if list.can_use_start < 0 {
		return nil
	}
	item := list.items[list.can_use_start]
	list.can_use_start += 1
	if list.can_use_start >= int32(len(list.items)) {
		list.can_use_start = -1
	}
	return item
}

func (list *SimpleItemPool) Recycle(item interface{}) bool {
	if list.can_use_start == 0 {
		return false
	}
	if list.can_use_start < 0 {
		list.can_use_start = int32(len(list.items)) - 1
	} else {
		list.can_use_start -= 1
	}
	list.items[list.can_use_start] = item
	return true
}

func (list *SimpleItemPool) HasFree() bool {
	return list.can_use_start >= 0
}
