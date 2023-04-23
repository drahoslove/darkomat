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
	Price     int64
	Count     float64 // NaN is '?' and Infinity is '∞'
	Timestamp time.Time
}

func (state *GiftState) MarshalJSON() ([]byte, error) {
	return json.Marshal(&struct {
		Price     int64 `json:"price"`
		Count     any   `json:"count"`
		Timestamp int64 `json:"time"` // epoch in seconds (JS Date firendly)
	}{
		state.Price,
		serializeCount(state.Count),
		state.Timestamp.Unix(),
	})
}

func (state *GiftState) UnmarshalJSON(data []byte) error {
	s := struct {
		Price     int64 `json:"price"`
		Count     any   `json:"count"`
		Timestamp int64 `json:"time"`
	}{}
	err := json.Unmarshal(data, &s)
	if err != nil {
		return err
	}
	*state = GiftState{
		Price:     s.Price,
		Count:     deserializeCount(s.Count),
		Timestamp: time.Unix(s.Timestamp, 0),
	}
	return nil
}

type Gift struct {
	Name     string
	Category string
	Url      string // consider this to be unique identifier
	Creation time.Time
	History  []GiftState
}

func (gift *Gift) MarshalJSON() ([]byte, error) {
	return json.Marshal(&struct {
		Name      string      `json:"name"`
		Category  string      `json:"category"`
		Url       string      `json:"url"` // epoch in milliseconds (JS Date firendly)
		CreatedAt int64       `json:"createdAt"`
		History   []GiftState `json:"history"`
	}{
		gift.Name,
		gift.Category,
		gift.Url,
		gift.Creation.Unix(),
		gift.History,
	})
}

func (gift *Gift) UnmarshalJSON(data []byte) error {
	s := struct {
		Name      string      `json:"name"`
		Category  string      `json:"category"`
		Url       string      `json:"url"`
		CreatedAt int64       `json:"createdAt"`
		History   []GiftState `json:"history"`
	}{}
	err := json.Unmarshal(data, &s)
	if err != nil {
		return err
	}

	*gift = Gift{
		Name:     s.Name,
		Category: s.Category,
		Url:      s.Url,
		Creation: time.Unix(s.CreatedAt, 0),
		History:  s.History,
	}
	return nil
}

func (gift *Gift) CurrentStock() any {
	return serializeCount(gift.History[len(gift.History)-1].Count)
}

func (gift *Gift) PreviousStock() any {
	if len(gift.History) < 2 {
		return "-"
	}
	currentState := gift.History[len(gift.History)-1]
	for i := len(gift.History) - 2; i >= 0; i-- {
		state := gift.History[i]
		if serializeCount(state.Count) != serializeCount(currentState.Count) {
			return serializeCount(state.Count)
		}
	}
	return "same"
}

func (gift *Gift) LastStockChangeAt() time.Time {
	if len(gift.History) < 2 {
		return time.Time{} // never
	}
	for i := len(gift.History) - 1; i > 0; i-- {
		state := gift.History[i]
		prevState := gift.History[i-1]
		if serializeCount(state.Count) != serializeCount(prevState.Count) {
			return state.Timestamp
		}
	}
	return time.Time{} // never
}

func (gift *Gift) CurrentPrice() string {
	if len(gift.History) < 1 {
		return "-"
	}
	return fmt.Sprint(gift.History[len(gift.History)-1].Price)
}

func (gift *Gift) PreviousPrice() string {
	if len(gift.History) < 2 {
		return "-"
	}
	currentState := gift.History[len(gift.History)-1]
	for i := len(gift.History) - 2; i >= 0; i-- {
		state := gift.History[i]
		if state.Price != currentState.Price {
			return fmt.Sprint(state.Price)
		}
	}
	return "same"
}

func (gift *Gift) LastPriceChangeAt() time.Time {
	if len(gift.History) < 2 {
		return time.Time{} // never
	}
	for i := len(gift.History) - 1; i > 0; i-- {
		state := gift.History[i]
		prevState := gift.History[i-1]
		if state.Price != prevState.Price {
			return state.Timestamp
		}
	}
	return gift.History[0].Timestamp
}

// update records state if
func (gift *Gift) record(state GiftState) bool {
	isNewState := len(gift.History) == 0
	if !isNewState {
		prev := gift.History[len(gift.History)-1]
		if prev.Price != state.Price {
			isNewState = true
		}
		if prev.Count != state.Count && !math.IsNaN(prev.Count) && !math.IsNaN(state.Count) {
			isNewState = true
		}
	}
	if isNewState {
		gift.History = append(gift.History, state)
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
		if gift.Url == url {
			return gift
		}
	}
	return nil
}

func (gifts *Gifts) FilterAdded(duration string) Gifts {
	list := Gifts{}
	d, _ := time.ParseDuration(duration)
	for _, gift := range *gifts {
		if gift.Creation.After(time.Now().Add(-d)) {
			list = append(list, gift)
		}
	}
	return list
}

func (gifts *Gifts) FilterDiscounted(duration string) Gifts {
	d, _ := time.ParseDuration(duration)
	return gifts.FilterStateChange(func(current, before GiftState) bool {
		return before.Price > current.Price
	}, d)
}

func (gifts *Gifts) FilterStockChanged(duration string) Gifts {
	d, _ := time.ParseDuration(duration)
	return gifts.FilterStateChange(func(current, before GiftState) bool {
		return !isSameCount(before.Count, current.Count)
	}, d)
}

func (gifts *Gifts) FilterStateChange(changed func(c, b GiftState) bool, duration time.Duration) Gifts {
	list := Gifts{}
	for _, gift := range *gifts {
		if len(gift.History) < 2 { // no state change in history -> skip
			continue
		}
		currentState := gift.History[len(gift.History)-1]
		if currentState.Timestamp.After(time.Now().Add(-duration)) { // state changed in d duration
			for i := len(gift.History) - 2; i >= 0; i-- { // compare to previsous prices
				state := gift.History[i]
				if changed(currentState, state) {
					list = append(list, gift)
					break
				}
			}
		}
	}
	return list
}

func (gifts *Gifts) loadJson() error {
	file, err := os.Open(JSON_FILE)
	if err != nil {
		return fmt.Errorf("failed to open json file, %s", err)
	}
	defer file.Close()
	encoder := json.NewDecoder(file)
	err = encoder.Decode(gifts)
	if err != nil {
		return fmt.Errorf("failed to endcode from json, %s", err)
	}
	return nil
}

// loads gifts from file gob file
func (gifts *Gifts) load() error {
	file, err := os.Open(GOB_FILE)
	if err != nil {
		return fmt.Errorf("failed to open gob file, %s", err)
	}
	defer file.Close()
	encoder := gob.NewDecoder(file)
	err = encoder.Decode(gifts)
	if err != nil {
		return fmt.Errorf("failed to decode from gob, %s", err)
	}
	return nil
}

// saves gifts to gob file
func (gifts *Gifts) save() error {
	file, err := os.Create(GOB_FILE)
	if err != nil {
		return fmt.Errorf("failed to open gob file, %s", err)
	}
	defer file.Close()
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

func isSameCount(a, b float64) bool {
	if math.IsNaN(a) && math.IsNaN(b) {
		return true
	}
	if math.IsInf(a, +1) && math.IsInf(b, +1) {
		return true
	}
	return a == b
}

func deserializeCount(count any) float64 {
	// Check if count is a special value "?"
	if count == "?" {
		return math.NaN()
	}

	// Check if count is a special value "∞"
	if count == "∞" {
		return math.Inf(1)
	}

	// Parse count as a float64
	countFloat, ok := count.(float64)
	if ok {
		return countFloat
	}

	return 0
}

func FormatTime(t time.Time) string {
	return strings.Split(t.String(), ":00 +")[0]
}
