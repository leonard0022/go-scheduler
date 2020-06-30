/*
  Package for finding game swaps.
*/

package main

import (
	"encoding/csv"
	"fmt"
	"io"
	"log"
	"os"
	"sort"
	"strings"
)

/*
 Application that reads in the schedule from GCMHA website and finds potential
 game swaps with eligible teams.

 Input information:
  1) team division (i.e. Novice A, Atom C, etc.)
  2) team name (i.e. GCTCOUGARS1)
  3) date(s) requiring swaps (i.e. 2020-03-01)

 General algorithm:
  1) eliminate incompatible divisions
  2) eliminate game days <- need division + team name

  Game switch alternatives:
  Atom A-C <-> Peewee B-C
  Peewee A <-> Bantam A-B
  Bantam A-B <-> Midget A-B
*/

var (
	swaps = map[string][]string{
		// atom a-c -> peewee b-c
		"atom a": {"atom a", "atom b", "atom c", "peewee b", "peewee c"},
		"atom b": {"atom a", "atom b", "atom c", "peewee b", "peewee c"},
		"atom c": {"atom a", "atom b", "atom c", "peewee b", "peewee c"},
		// peewee a -> bantam a-b
		"peewee a": {"peewee a", "bantam a", "bantam b"},
		// peewee b-c -> atom a-c
		"peewee b": {"peewee b", "peewee c", "atom a", "atom b", "atom c"},
		"peewee c": {"peewee b", "peewee c", "atom a", "atom b", "atom c"},
		// bantam a-b -> peewee a
		"bantam a": {"bantam a", "bantam b", "peewee a", "midget a", "midget b"},
		"bantam b": {"bantam a", "bantam b", "peewee a", "midget a", "midget b"},
		// midget a-b -> bantan a-b
		"midget a": {"bantam a", "bantam b", "midget a", "midget b"},
		"midget b": {"bantam a", "bantam b", "midget a", "midget b"},
	}
)

/*
 Use everything in the string up to the first space as the team name. Normalize
 everything to uppercase. Check to see if the string is already in the list. If
 so then return the original list; otherwise, append the new string and return
 the updated list.
*/
func addUnique(list []string, str string) []string {
	tStr := strings.ToUpper(strings.Fields(str)[0])

	for _, v := range list {
		if strings.ToUpper(v) == tStr {
			return list
		}
	}

	list = append(list, tStr)
	return list
}

func main() {

	/*
	 Expecting CSV records containing:
	*/
	type Record struct {
		division string // index 0
		game     string // index 1
		date     string // index 2
		time     string // index 3
		location string // index 4
		team1    string // index 5 - can contain score: (1)
		team2    string // index 6 - can contain score: (1)
	}

	//--------------------------------------------------------------------------
	// Get team division

	// sort the keys
	var keys []string
	for k := range swaps {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	// prompt for team division
	idx := 0
	for _, k := range keys {
		fmt.Printf("%d - %s\n", idx, k)
		idx++
	}

	fmt.Print("Select division: ")
	_, err := fmt.Scanln(&idx)
	if err != nil {
		log.Fatal(err)
	}
	division := keys[idx]
	fmt.Println("Your division: ", division)
	fmt.Println("Your potential swaps: ", swaps[division])

	// prompt for filename
	fName := "missing"
	fmt.Print("Enter filename: ")
	_, err = fmt.Scanln(&fName)
	if err != nil {
		log.Fatal(err)
	}

	// open file for reading
	fmt.Println("opening file:", fName)
	f, err := os.Open(fName)
	defer f.Close()
	if err != nil {
		log.Fatal(err)
	}

	// read all lines from
	reader := csv.NewReader(f)

	// list of records
	var records [][]string

	// used to store the list of team names
	var teams []string

	// loop reading each record
	for cnt := 1; ; {
		record, err := reader.Read()

		// stop at end of file
		if err == io.EOF {
			break
		}

		// exit if error found
		if err != nil {
			log.Fatal(err)
		}

		// check if entry is part of a division in which swaps are allowed
		for _, v := range swaps[division] {
			// extract names of teams in SAME division
			if strings.HasPrefix(strings.ToLower(record[0]), division) {
				for i := 5; i <= 6; i++ {
					teams = addUnique(teams, record[i])
				}
			}

			// store records from ALL COMPATIBLE divisions
			if strings.HasPrefix(strings.ToLower(record[0]), v) {
				records = append(records, record)
				//fmt.Printf("%05d: %d: %s\n", cnt, len(record), record)
				break
			}
		}

		//fmt.Printf("%05d: %d: %s\n", cnt, len(record), record)

		cnt++
	}

	// prompt for team
	idx = 0
	for _, k := range teams {
		fmt.Printf("%d - %s\n", idx, k)
		idx++
	}
	fmt.Print("Select team: ")
	_, err = fmt.Scanln(&idx)
	if err != nil {
		log.Fatal(err)
	}
	team := teams[idx]
	fmt.Println("Your team: ", team)

	// Get the dates of the game swap
	// todo validate the date format the user entered
	//      1. current or future date
	//      2. month and day values are valid
	var date string
	fmt.Print("Enter date (i.e. YYYY-MM-DD)")
	_, err = fmt.Scanln(&date)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("Your date: ", date)

	// 1) find all teams (division + team name) playing on the given date
	// 2) remove your opponent from the list of teams
	// 3) eliminate all teams playing on the given date
}
