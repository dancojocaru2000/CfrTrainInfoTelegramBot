package utils

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

var (
	InvalidDateFormat = fmt.Errorf("invalid date format")
)

func ParseDate(input string) (time.Time, error) {
	if strings.Contains(input, "-") {
		return parse3Part(input, "-", 0, 1, 2)
	} else if strings.Contains(input, "/") {
		return parse3Part(input, "/", 2, 0, 1)
	} else if strings.Contains(input, ".") {
		return parse3Part(input, ".", 2, 1, 0)
	} else {
		parsed, err := strconv.ParseInt(input, 10, 63)
		if err != nil {
			return time.Time{}, err
		}
		return time.Unix(parsed, 0), nil
	}
}

func parse3Part(input string, sep string, yearIndex int, monthIndex int, dayIndex int) (time.Time, error) {
	splitted := strings.Split(input, sep)
	if len(splitted) == 2 && yearIndex == 2 {
		// If the year is the last part of the format, allow omitting it
		splitted = append(splitted, fmt.Sprintf("%d", time.Now().Year()))
	}
	if len(splitted) != 3 {
		return time.Time{}, InvalidDateFormat
	}
	year, err := strconv.Atoi(splitted[yearIndex])
	if err != nil {
		return time.Time{}, InvalidDateFormat
	}
	if year < 100 {
		// Assume xx.xx.23 or x/x/23 => 2023
		year = (time.Now().Year() / 100 * 100) + year
	}
	month, err := strconv.Atoi(splitted[monthIndex])
	if err != nil {
		return time.Time{}, InvalidDateFormat
	}
	day, err := strconv.Atoi(splitted[dayIndex])
	if err != nil {
		return time.Time{}, InvalidDateFormat
	}
	return time.Date(year, time.Month(month), day, 12, 0, 0, 0, Location), nil
}
