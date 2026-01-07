package ton

import (
	"context"
	"strings"
	"math/rand"
	"time"
)

var KnownProviders = []string{
	"6f6053b4435ffd17bb1368d85ac3573b6fee2d62bb12629f1ddd36cbd7eb692d",
	"516a32d046c4a2187088143edf278a6be1a119dc691bb7bd21dd81ed26a26a34",
	"7a156635aeae3a97e2947ba88385390e7aefbb70d789d2f6916c9423d4697f3a",
}


func (s *Service) FindRandomProvider(ctx context.Context, exclude []string) (string, error) {

	excludedMap := make(map[string]bool)
	for _, addr := range exclude {
		if len(addr) > 64 && strings.Contains(addr, ":") {
			parts := strings.Split(addr, ":")
			if len(parts) == 2 {
				excludedMap[parts[1]] = true
			}
		} else {
			excludedMap[addr] = true
		}
	}

	var candidates []string
	for _, provider := range KnownProviders {
		if !excludedMap[provider] {
			candidates = append(candidates, provider)
		}
	}

	if len(candidates) == 0 {
		rng := rand.New(rand.NewSource(time.Now().UnixNano()))
		return KnownProviders[rng.Intn(len(KnownProviders))], nil
	}

	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	return candidates[rng.Intn(len(candidates))], nil
}