package main

import (
	"encoding/csv"
	"encoding/gob"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

// the values of of the Gifts at specific opint in time
type GiftState struct {
	price     int64
	count     float64 // NaN is '?' and Infinity is '∞'
	timestamp time.Time
}

func (state *GiftState) MarshalJSON() ([]byte, error) {
	return json.Marshal(&struct {
		Price     int64 `json:"price"`
		Count     any   `json:"count"`
		Timestamp int64 `json:"time"` // epoch in seconds (JS Date firendly)
	}{
		state.price,
		serializeCount(state.count),
		state.timestamp.Unix(),
	})
}

type Gift struct {
	name     string
	category string
	url      string // consider this to be unique identifier
	creation time.Time
	history  []GiftState
}

func (gift *Gift) MarshalJSON() ([]byte, error) {
	return json.Marshal(&struct {
		Name      string      `json:"name"`
		Category  string      `json:"category"`
		Url       string      `json:"url"` // epoch in milliseconds (JS Date firendly)
		CreatedAt int64       `json:"createdAt"`
		History   []GiftState `json:"history"`
	}{
		gift.name,
		gift.category,
		gift.url,
		gift.creation.Unix(),
		gift.history,
	})
}

func (gift *Gift) CurrentStock() any {
	return serializeCount(gift.history[len(gift.history)-1].count)
}

func (gift *Gift) PreviousStock() any {
	if len(gift.history) < 2 {
		return "-"
	}
	currentState := gift.history[len(gift.history)-1]
	for i := len(gift.history) - 2; i >= 0; i-- {
		state := gift.history[i]
		if serializeCount(state.count) != serializeCount(currentState.count) {
			return serializeCount(state.count)
		}
	}
	return "same"
}

func (gift *Gift) LastStockChangeAt() time.Time {
	if len(gift.history) < 2 {
		return time.Time{} // never
	}
	for i := len(gift.history) - 1; i > 0; i-- {
		state := gift.history[i]
		prevState := gift.history[i-1]
		if serializeCount(state.count) != serializeCount(prevState.count) {
			return state.timestamp
		}
	}
	return gift.history[0].timestamp
}

func (gift *Gift) CurrentPrice() string {
	if len(gift.history) < 1 {
		return "-"
	}
	return fmt.Sprint(gift.history[len(gift.history)-1].price)
}

func (gift *Gift) PreviousPrice() string {
	if len(gift.history) < 2 {
		return "-"
	}
	currentState := gift.history[len(gift.history)-1]
	for i := len(gift.history) - 2; i >= 0; i-- {
		state := gift.history[i]
		if state.price != currentState.price {
			return fmt.Sprint(state.price)
		}
	}
	return "same"
}

func (gift *Gift) LastPriceChangeAt() time.Time {
	if len(gift.history) < 2 {
		return time.Time{} // never
	}
	for i := len(gift.history) - 1; i > 0; i-- {
		state := gift.history[i]
		prevState := gift.history[i-1]
		if state.price != prevState.price {
			return state.timestamp
		}
	}
	return gift.history[0].timestamp
}

func (gift *Gift) Url() string {
	return gift.url
}

func (gift *Gift) Name() string {
	return gift.name
}
func (gift *Gift) Created() string {
	return strings.Split(gift.creation.String(), ":00 +")[0]
}

// update records state if
func (gift *Gift) record(state GiftState) bool {
	isNewState := len(gift.history) == 0
	if !isNewState {
		prev := gift.history[len(gift.history)-1]
		if prev.price != state.price {
			isNewState = true
		}
		if prev.count != state.count && !math.IsNaN(prev.count) && !math.IsNaN(state.count) {
			isNewState = true
		}
	}
	if isNewState {
		gift.history = append(gift.history, state)
	}
	return isNewState
}

type Gifts []*Gift

// add add gift to list
func (g *Gifts) push(gift *Gift) {
	*g = append(*g, gift)
}

// find Gift with given url
func (g *Gifts) find(url string) *Gift {
	for _, gift := range *g {
		if gift.url == url {
			return gift
		}
	}
	return nil
}

func (gifts *Gifts) FilterAdded(duration string) Gifts {
	list := Gifts{}
	d, _ := time.ParseDuration(duration)
	for _, gift := range *gifts {
		if gift.creation.After(time.Now().Add(-d)) {
			list = append(list, gift)
		}
	}
	return list
}

func (gifts *Gifts) FilterDiscounted(duration string) Gifts {
	d, _ := time.ParseDuration(duration)
	return gifts.FilterStateChange(func(current, before GiftState) bool {
		return before.price > current.price
	}, d)
}

func (gifts *Gifts) FilterStockChanged(duration string) Gifts {
	d, _ := time.ParseDuration(duration)
	return gifts.FilterStateChange(func(current, before GiftState) bool {
		return serializeCount(before.count) != serializeCount(current.count)
	}, d)
}

func (gifts *Gifts) FilterStateChange(changed func(c, b GiftState) bool, duration time.Duration) Gifts {
	list := Gifts{}
	for _, gift := range *gifts {
		if len(gift.history) < 2 { // no state change in history -> skip
			continue
		}
		currentState := gift.history[len(gift.history)-1]
		for i, state := range gift.history { // compare to previsous prices
			if i == len(gift.history)-1 { // skip last
				break
			}
			if state.timestamp.After(time.Now().Add(-duration)) { // state changed in d duration
				if changed(currentState, state) {
					list = append(list, gift)
				}
			}
		}
	}
	return list
}

// loads gifts from file gob file
func (gifts *Gifts) load() error {
	file, err := os.Open(GOB_FILE)
	if err != nil {
		return fmt.Errorf("failed to open gob file, %s", err)
	}
	encoder := gob.NewDecoder(file)
	err = encoder.Decode(gifts)
	if err != nil {
		return fmt.Errorf("failed to endcode to gob, %s", err)
	}
	return nil
}

// saves gifts to gob file
func (gifts *Gifts) save() error {
	file, err := os.Create(GOB_FILE)
	if err != nil {
		return fmt.Errorf("failed to open gob file, %s", err)
	}
	encoder := gob.NewEncoder(file)
	err = encoder.Encode(gifts)
	if err != nil {
		return fmt.Errorf("failed to endcode to gob, %s", err)
	}
	return nil
}

// fetch the gifts from the catalogue and update itselves
func (gifts *Gifts) refresh(timestamp time.Time) error {
	// fetch the cvs
	res, err := http.Get(CSV_URL)
	if err != nil {
		return fmt.Errorf("failed to fetch scv file, %s", err)
	}

	// parse to lines of strings
	reader := csv.NewReader(res.Body)
	lines, err := reader.ReadAll()
	if err != nil {
		return fmt.Errorf("failed to read scv file, %s", err)
	}

	changed := false

	for i, line := range lines {
		if i == 0 { // skip the header line
			continue
		}
		const ( // order of the columns
			Druh = iota
			Název
			Kačky
			Počet
			Vznik
			Odkaz
		)
		// parse columns
		category := line[Druh]
		name := line[Název]
		url := line[Odkaz]
		price, _ := strconv.ParseInt(line[Kačky], 10, 64)
		creation, _ := time.Parse("2006-01-02", line[Vznik])
		count := parseCount(line[Počet])

		state := GiftState{price, count, timestamp} // the current state to be recorded

		gift := gifts.find(url)

		if gift == nil { // create new
			gift = &Gift{
				name,
				category,
				url,
				creation,
				make([]GiftState, 0),
			}
			gifts.push(gift)
		}
		// update with history
		if gift.record(state) {
			changed = true
		}
	}
	if changed {
		gifts.save()
	}
	return nil
}

// parse the Počet value to float number
func parseCount(str string) float64 {
	switch str {
	case "?":
		return math.NaN()
	case "∞":
		return math.Inf(+1)
	default:
		count, _ := strconv.ParseFloat(str, 64)
		return count
	}
}

// return json compatible representation of the count number
func serializeCount(count float64) any {
	if math.IsNaN(count) {
		return "?"
	}
	if math.IsInf(count, +1) {
		return "∞"
	}
	return count
}
