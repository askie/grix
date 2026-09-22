package handler

func containsInt64(list []int64, target int64) bool {
	for _, v := range list {
		if v == target {
			return true
		}
	}
	return false
}

func intersectTargetUserIDs(targetUserIDs, allowedUserIDs []int64) []int64 {
	if len(targetUserIDs) == 0 || len(allowedUserIDs) == 0 {
		return nil
	}
	allowed := make(map[int64]struct{}, len(allowedUserIDs))
	for _, id := range allowedUserIDs {
		if id > 0 {
			allowed[id] = struct{}{}
		}
	}
	filtered := make([]int64, 0, len(targetUserIDs))
	for _, id := range targetUserIDs {
		if _, ok := allowed[id]; ok {
			filtered = append(filtered, id)
		}
	}
	return dedupePositiveTargetUserIDs(filtered)
}
