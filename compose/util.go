package compose

func CloneByteSlices(src [][]byte) [][]byte {
	if src == nil {
		return nil
	}
	out := make([][]byte, len(src))
	for i, b := range src {
		if b == nil {
			out[i] = nil
			continue
		}
		out[i] = append([]byte(nil), b...)
	}
	return out
}
