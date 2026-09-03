package world

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

func direction(fromX, fromY, toX, toY int) int {
	dir := 4
	flagX := 0
	if fromX < toX {
		flagX = 1
	} else if fromX > toX {
		flagX = -1
	}
	if abs(fromY-toY) > 2 && fromX >= toX-1 && fromX <= toX+1 {
		flagX = 0
	}
	flagY := 0
	if fromY < toY {
		flagY = 1
	} else if fromY > toY {
		flagY = -1
	}
	if abs(fromX-toX) > 2 && fromY > toY-1 && fromY <= toY+1 {
		flagY = 0
	}
	for candidate, off := range dirOffsets {
		if off[0] == flagX && off[1] == flagY {
			dir = candidate
			break
		}
	}
	return dir
}

func Direction(fromX, fromY, toX, toY int) int {
	return direction(fromX, fromY, toX, toY)
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
