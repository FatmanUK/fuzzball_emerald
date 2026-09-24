package timefmt

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Format renders a time under a C strftime format, covering the
// subset MUF and MPI programs use — MUF's TIMEFMT and MPI's
// {ftime}.
func Format(format string, t time.Time) string {
	var b strings.Builder
	for i := 0; i < len(format); i++ {
		if format[i] != '%' || i+1 >= len(format) {
			b.WriteByte(format[i])
			continue
		}
		i++
		switch format[i] {
		case 'Y':
			b.WriteString(strconv.Itoa(t.Year()))
		case 'y':
			b.WriteString(fmt.Sprintf("%02d", t.Year()%100))
		case 'm':
			b.WriteString(fmt.Sprintf("%02d", int(t.Month())))
		case 'd':
			b.WriteString(fmt.Sprintf("%02d", t.Day()))
		case 'e':
			b.WriteString(fmt.Sprintf("%2d", t.Day()))
		case 'H':
			b.WriteString(fmt.Sprintf("%02d", t.Hour()))
		case 'M':
			b.WriteString(fmt.Sprintf("%02d", t.Minute()))
		case 'S':
			b.WriteString(fmt.Sprintf("%02d", t.Second()))
		case 'A':
			b.WriteString(t.Weekday().String())
		case 'a':
			b.WriteString(t.Weekday().String()[:3])
		case 'B':
			b.WriteString(t.Month().String())
		case 'b', 'h':
			b.WriteString(t.Month().String()[:3])
		case 'Z':
			zone, _ := t.Zone()
			b.WriteString(zone)
		case 'p':
			if t.Hour() < 12 {
				b.WriteString("AM")
			} else {
				b.WriteString("PM")
			}
		case 'I':
			h := t.Hour() % 12
			if h == 0 {
				h = 12
			}
			b.WriteString(fmt.Sprintf("%02d", h))
		case 'j':
			b.WriteString(fmt.Sprintf("%03d", t.YearDay()))
		case 'T':
			b.WriteString(t.Format("15:04:05"))
		case 'D':
			b.WriteString(t.Format("01/02/06"))
		case 'c':
			b.WriteString(t.Format("Mon Jan  2 15:04:05 2006"))
		case 'n':
			b.WriteByte('\n')
		case 't':
			b.WriteByte('\t')
		case '%':
			b.WriteByte('%')
		default:
			b.WriteByte('%')
			b.WriteByte(format[i])
		}
	}
	return b.String()
}
