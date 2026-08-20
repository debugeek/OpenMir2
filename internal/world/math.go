package world

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

func direction(fromX, fromY, toX, toY int) int {
	dx := sign(toX - fromX)
	dy := sign(toY - fromY)
	for dir, off := range dirOffsets {
		if off[0] == dx && off[1] == dy {
			return dir
		}
	}
	return 0
}

func sign(v int) int {
	if v < 0 {
		return -1
	}
	if v > 0 {
		return 1
	}
	return 0
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
