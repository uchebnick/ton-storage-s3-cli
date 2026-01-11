package ton

import (
	"context"
	"strings"
	"math/rand"
	"time"
)

var KnownProviders = []string{
	"3248d343499f04515738e96c05c52852cb093df2d4519c2b36e0d170761881b8",
	"7f41f5e1ca22563d01cc9106d8ed14c76ae643aeac9a51f889d1301577556b63",
	"872cb0255b678d3e94cb60d7e18f8cf45eb99d7742fbd56411e7e8349296fbea",
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