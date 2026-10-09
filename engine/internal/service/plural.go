package service

// ruPlural picks the Russian word for a count: one for 1, 21, 101 («раз»,
// «запись»), few for 2–4, 22–24 («раза», «записи»), many for 0, 5–20,
// 11–14, 111 («раз», «записей»).
func ruPlural(n int, one, few, many string) string {
	if n < 0 {
		n = -n
	}
	switch m10, m100 := n%10, n%100; {
	case m10 == 1 && m100 != 11:
		return one
	case m10 >= 2 && m10 <= 4 && (m100 < 12 || m100 > 14):
		return few
	}
	return many
}
