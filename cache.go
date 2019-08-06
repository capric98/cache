func OUT(s unsafe.Pointer) interface{} {
	return (*(*interface{})(s))
}

func Insert(data interface{}, pool []byte) {
	us := unsafe.Pointer(&data)
	len := int(unsafe.Sizeof(data))
	fakeslice := reflect.SliceHeader{
		Data: uintptr(us),
		Len:  len,
		Cap:  len,
	}
	copy(pool, *(*[]byte)(unsafe.Pointer(&fakeslice)))
}