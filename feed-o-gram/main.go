package main

import (
	"encoding/csv"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type Log struct {
	LogType     string
	Date        string
	Time        string
	Description *string
}

func (l Log) ToSlice() []string {
	slice := []string{l.LogType, l.Date, l.Time}
	if l.Description != nil {
		slice = append(slice, *l.Description)
	}
	return slice
}

const mealIcon = "||"

func main() {
	flag.Parse()

	rawEntry := flag.Args()

	if len(rawEntry) == 0 {
		fmt.Println("Error: No input provided")
		return
	}

	// parse into Input
	log := Log{}

	switch rawEntry[0] {
	case "meal":
		log.LogType = "meal"
		log.Date = time.Now().Format("2006-01-02")

		// TODO: should be able to parse time like 2pm, human friendly time
		if len(rawEntry) < 2 {
			log.Time = time.Now().Format("15:04")
		} else {
			log.Time = rawEntry[1]
		}
		if len(rawEntry) > 2 && rawEntry[2] != "" {
			log.Description = &rawEntry[2]
		}
		err := Write(log)
		if err != nil {
			fmt.Println("Error: ", err)
		}
	case "view":
		logs, err := Read()
		if err != nil {
			fmt.Println("Error: ", err)
		}
		// create map with log entries for each date and send to PrintFeedogram
		dateTimes := make(map[string][]string)
		for _, log := range logs {
			if log.LogType == "meal" {
				if _, ok := dateTimes[log.Date]; !ok {
					dateTimes[log.Date] = make([]string, 0)
				}
				dateTimes[log.Date] = append(dateTimes[log.Date], log.Time)
			}
		}
		for date, times := range dateTimes {
			fmt.Println(PrintFeedogram(date, times))
		}

	default:
		fmt.Println("Error: Invalid input")
		return
	}
}

func Read() ([]Log, error) {
	file, err := os.Open("feed-o-gram.csv")
	if err != nil {
		return nil, fmt.Errorf("error opening data file: %v", err)
	}
	defer file.Close()

	reader := csv.NewReader(file)
	records, err := reader.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("error reading data file: %v", err)
	}

	logs := make([]Log, 0, len(records))
	for _, record := range records {
		log := Log{
			LogType: record[0],
			Date:    record[1],
			Time:    record[2],
		}
		if len(record) > 3 {
			log.Description = &record[3]
		}
		logs = append(logs, log)
	}

	return logs, nil
}

func Write(log Log) error {
	file, err := os.OpenFile("feed-o-gram.csv", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("error opening data file: %v", err)
	}
	defer file.Close()

	writer := csv.NewWriter(file)
	defer writer.Flush()

	err = writer.Write(log.ToSlice())
	if err != nil {
		return fmt.Errorf("error writing to file: %v", err)
	}
	return nil
}

func PrintFeedogram(date string, times []string) string {
	// TODO adjust to current terminal width
	lineLength := 80
	mealsMinutes := make([]int, 0)
	for _, timeOfDay := range times {
		hoursStr, minutesStr := strings.Split(timeOfDay, ":")[0], strings.Split(timeOfDay, ":")[1]
		hours, err := strconv.Atoi(hoursStr)
		if err != nil {
			fmt.Println("Error: ", err)
		}
		minutes, err := strconv.Atoi(minutesStr)
		if err != nil {
			fmt.Println("Error: ", err)
		}
		minutesFromDayStart := hours*60 + minutes
		mealsMinutes = append(mealsMinutes, minutesFromDayStart)
	}
	result := strings.Repeat("-", lineLength-len(mealIcon)*2*len(mealsMinutes))
	minutesInDay := 24 * 60
	for _, mealMinutes := range mealsMinutes {
		// add mealIcon in result based on mealMinutes/minutesInDay * lineLength
		mealPosition := int(float64(mealMinutes) / float64(minutesInDay) * float64(lineLength))
		result = result[:mealPosition] + mealIcon + result[mealPosition:]
	}

	return date + " " + result
}
