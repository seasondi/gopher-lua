package lua

import (
	"container/list"
)

const defaultArrayCap = 0
const defaultHashCap = 0

type lValueArraySorter struct {
	L      *LState
	Fn     *LFunction
	Values []LValue
}

func (lv lValueArraySorter) Len() int {
	return len(lv.Values)
}

func (lv lValueArraySorter) Swap(i, j int) {
	lv.Values[i], lv.Values[j] = lv.Values[j], lv.Values[i]
}

func (lv lValueArraySorter) Less(i, j int) bool {
	if lv.Fn != nil {
		lv.L.Push(lv.Fn)
		lv.L.Push(lv.Values[i])
		lv.L.Push(lv.Values[j])
		lv.L.Call(2, 1)
		return LVAsBool(lv.L.reg.Pop())
	}
	return lessThan(lv.L, lv.Values[i], lv.Values[j])
}

func newLTable(acap int, hcap int) *LTable {
	if acap < 0 {
		acap = 0
	}
	if hcap < 0 {
		hcap = 0
	}
	tb := &LTable{
		Metatable: LNil,
		array:     make([]LValue, 0, acap),
		dict:      make(map[LValue]LValue, hcap),
		keys:      list.New(),
		k2l:       make(map[LValue]*list.Element),
	}
	return tb
}

// Len returns length of this LTable without using __len.
func (tb *LTable) Len() int {
	var prev LValue = LNil
	for i := len(tb.array) - 1; i >= 0; i-- {
		v := tb.array[i]
		if prev == LNil && v != LNil {
			return i + 1
		}
		prev = v
	}
	return 0
}

// Append appends a given LValue to this LTable.
func (tb *LTable) Append(value LValue) {
	if value == LNil {
		return
	}
	if len(tb.array) == 0 || tb.array[len(tb.array)-1] != LNil {
		tb.array = append(tb.array, value)
	} else {
		i := len(tb.array) - 2
		for ; i >= 0; i-- {
			if tb.array[i] != LNil {
				break
			}
		}
		tb.array[i+1] = value
	}
}

// Insert inserts a given LValue at position `i` in this table.
func (tb *LTable) Insert(i int, value LValue) {
	if i > len(tb.array) {
		tb.RawSetInt(i, value)
		return
	}
	if i <= 0 {
		tb.RawSetH(LNumber(i), value)
		return
	}
	i -= 1
	tb.array = append(tb.array, LNil)
	copy(tb.array[i+1:], tb.array[i:])
	tb.array[i] = value
}

// MaxN returns a maximum number key that nil value does not exist before it.
func (tb *LTable) MaxN() int {
	if tb.array == nil {
		return 0
	}
	for i := len(tb.array) - 1; i >= 0; i-- {
		if tb.array[i] != LNil {
			return i + 1
		}
	}
	return 0
}

// Remove removes from this table the element at a given position.
func (tb *LTable) Remove(pos int) LValue {
	larray := len(tb.array)
	if larray == 0 {
		return LNil
	}
	i := pos - 1
	oldval := LNil
	switch {
	case i >= larray || i < 0:
		// nothing to do
	case i == larray-1:
		oldval = tb.array[larray-1]
		tb.array = tb.array[:larray-1]
	default:
		oldval = tb.array[i]
		copy(tb.array[i:], tb.array[i+1:])
		tb.array[larray-1] = nil
		tb.array = tb.array[:larray-1]
	}
	return oldval
}

// RawSet sets a given LValue to a given index without the __newindex metamethod.
// It is recommended to use `RawSetString` or `RawSetInt` for performance
// if you already know the given LValue is a string or number.
func (tb *LTable) RawSet(key LValue, value LValue) {
	if key.Type() == LTNumber && isInteger(key.(LNumber)) {
		tb.RawSetInt(int(key.(LNumber)), value)
	} else {
		tb.RawSetH(key, value)
	}
}

// RawSetInt sets a given LValue at a position `key` without the __newindex metamethod.
func (tb *LTable) RawSetInt(key int, value LValue) {
	if key < 1 {
		tb.RawSetH(LNumber(key), value)
		return
	}

	if value == LNil {
		if tb.array != nil && key <= len(tb.array) {
			if v := tb.array[key-1]; v != LNil {
				tb.array[key-1] = value
				return
			}
		}
		tb.RawSetH(LNumber(key), value)
		return
	}

	if !tb.canPutInArray(key) {
		tb.RawSetH(LNumber(key), value)
		return
	}

	if tb.array == nil {
		tb.array = make([]LValue, 0)
		tb.arrayContinuousLen = 0
	}

	alen := len(tb.array)
	for i := tb.arrayContinuousLen + 1; i < key; i++ {
		if i < alen && tb.array[i] != LNil {
			//already in array
			continue
		} else {
			if i < alen {
				tb.array[i] = tb.dict[LNumber(key)]
			} else {
				tb.array = append(tb.array, LNumber(key))
			}
			tb.removeFromHash(LNumber(key))
		}
	}
	tb.array = append(tb.array, value)
	rightCount, next := 0, key
	for {
		next += 1
		if v, find := tb.dict[LNumber(next)]; find {
			tb.array = append(tb.array, v)
			tb.removeFromHash(LNumber(next))
			rightCount += 1
		} else {
			break
		}
	}
	tb.arrayContinuousLen = key + rightCount
}

// RawSetString sets a given LValue to a given string index without the __newindex metamethod.
func (tb *LTable) RawSetString(key string, value LValue) {
	tb.RawSetH(LString(key), value)
}

// RawSetH sets a given LValue to a given index without the __newindex metamethod.
func (tb *LTable) RawSetH(key LValue, value LValue) {
	if value == LNil {
		tb.removeFromHash(key)
	} else {
		tb.addToHash(key, value)
	}
}

// RawGet returns an LValue associated with a given key without __index metamethod.
func (tb *LTable) RawGet(key LValue) LValue {
	if key.Type() == LTNumber && isInteger(key.(LNumber)) {
		return tb.RawGetInt(int(key.(LNumber)))
	} else {
		return tb.rawGetH(key)
	}
}

// RawGetInt returns an LValue at position `key` without __index metamethod.
func (tb *LTable) RawGetInt(key int) LValue {
	index := int(key) - 1
	if index >= len(tb.array) || index < 0 {
		return tb.rawGetH(LNumber(key))
	}
	return tb.array[index]
}

// RawGet returns an LValue associated with a given key without __index metamethod.
func (tb *LTable) rawGetH(key LValue) LValue {
	if val, ok := tb.dict[key]; ok {
		return val
	} else {
		return LNil
	}
}

// RawGetString returns an LValue associated with a given key without __index metamethod.
func (tb *LTable) RawGetString(key string) LValue {
	return tb.rawGetH(LString(key))
}

// ForEach iterates over this table of elements, yielding each in turn to a given function.
func (tb *LTable) ForEach(cb func(LValue, LValue)) {
	for i, v := range tb.array {
		if v != LNil {
			cb(LNumber(i+1), v)
		}
	}
	for k, v := range tb.dict {
		if v != LNil {
			cb(k, v)
		}
	}
}

// This function is equivalent to lua_next ( http://www.lua.org/manual/5.1/manual.html#lua_next ).
func (tb *LTable) Next(key LValue) (LValue, LValue) {
	if key == LNil {
		key = LNumber(0)
	}

	if k, ok := key.(LNumber); ok && isInteger(k) && k >= 0 {
		idx := int(k)
		for ; idx < len(tb.array); idx++ {
			if v := tb.array[idx]; v != LNil {
				return LNumber(idx + 1), v
			}
		}
		if idx == len(tb.array) {
			if len(tb.dict) == 0 {
				return LNil, LNil
			}
			key = tb.keys.Front().Value.(LValue)
			if v := tb.rawGetH(key); v != LNil {
				return key, v
			}
		}
	}

	var nextKey LValue
	if k, ok := tb.k2l[key]; !ok {
		//遍历过程中,key已删除
		if el := tb.keys.Front(); el != nil {
			nextKey = el.Value.(LValue)
		}
	} else {
		if el := k.Next(); el != nil {
			nextKey = el.Value.(LValue)
		}
	}

	if nextKey != nil {
		for e := tb.k2l[nextKey]; e != nil; e = e.Next() {
			k := e.Value.(LValue)
			if v := tb.rawGetH(k); v != LNil {
				return k, v
			}
		}
	}

	return LNil, LNil
}

func (tb *LTable) addToHash(key, value LValue) {
	tb.dict[key] = value
	e := tb.keys.PushBack(key)
	tb.k2l[key] = e
}

func (tb *LTable) removeFromHash(key LValue) {
	if e, ok := tb.k2l[key]; ok {
		tb.keys.Remove(e)
		delete(tb.k2l, key)
	}
	delete(tb.dict, key)
}

func (tb *LTable) canPutInArray(key int) bool {
	if tb.array == nil {
		return key == 1
	}

	alen := len(tb.array)
	for i := key - 1; i > tb.arrayContinuousLen; i-- {
		if i > alen || tb.array[i-1] == LNil {
			if _, find := tb.dict[LNumber(i)]; !find {
				return false
			}
		}
	}
	return true
}
