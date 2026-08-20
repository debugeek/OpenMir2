package world

import (
	"strconv"
	"strings"

	"openmir2/internal/storage"
)

func CharacterActorID(ch storage.Character) int32 {
	if _, suffix, ok := strings.Cut(ch.ID, "-"); ok {
		if n, err := strconv.Atoi(suffix); err == nil && n > 0 {
			return int32(n)
		}
	}
	return 1
}
