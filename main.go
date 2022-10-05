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
	"regexp"
	"strings"
)

/*
 Application that reads in the schedule from GCMHA website and finds potential
 game swaps with eligible teams.

 Input information:
  1) team division (i.e. Novice A, Atom C, etc.)
    - user selected from static list
  2) swappable divisions
    - static data selected based on team division
  3) date(s) requiring swaps (i.e. 2020-03-01)
    - user entered (free format)
    - will eliminate all teams playing on that date
  4) schedule (want to get this from online)
    - csv format: division, game, date, time, location, home, away, +extra
  5) teams to eliminate
    - generated from schedule based on: date, swappable division
  6) team name (i.e. GCTCOUGARS1)
    - user select from list generate from schedule
  7) email addresses (want to get this from online)

 General algorithm:
  1) eliminate played games
  2) eliminate incompatible divisions
  3) eliminate game days for teams in game being swapped <- need division + team name
  4) elimite teams playing on the day of the game you want to swap

 Game switch alternatives:
  U11 A-C <-> U13 B-C
  U13 A <-> U15 A-B
  U15 A-B <-> U18 A-B
*/

type division_type struct {
	name       string // name of the division
	nameRegex  string // regex for matching division
	swaps      string // description of swaps
	swapsRegex string // regular expression for finding swaps
}

var (
	// Contains division names and rules for swapping games
	divisions = []division_type{
		{"U11 A", "U11.*A", "U11 A -> U11 A-C, U13 B-C", "U11.*[A-C]|U13.*[B-C]"},
		{"U11 B", "U11.*B", "U11 B -> U11 A-C, U13 B-C", "U11.*[A-C]|U13.*[B-C]"},
		{"U11 C", "U11.*C", "U11 C -> U11 A-C, U13 B-C", "U11.*[A-C]|U13.*[B-C]"},
	}
)

/*
Normalize everything to uppercase. Check to see if the string is already in
the list. If so then return the original list; otherwise, append the new
string and return the updated list.
*/
func addUnique(list []string, str string) []string {
	tStr := strings.ToUpper(str)

	for _, v := range list {
		if strings.ToUpper(v) == tStr {
			return list
		}
	}

	list = append(list, tStr)
	return list
}

func main() {

	// Get team division
	fmt.Println("Select your division: ")
	dIdx := 0
	for _, k := range divisions {
		fmt.Printf(" %d - %s\n", dIdx, k.name)
		dIdx++
	}

	/* TODO - testing new format, remove this
	// sort the divisions
	var divisions []string
	for k := range swap_rules {
		divisions = append(divisions, k)
	}
	sort.Strings(divisions)

	// prompt for team division
	fmt.Println("Select your division: ")
	idx := 0
	for _, k := range divisions {
		fmt.Printf(" %d - %s\n", idx, k)
		idx++
	}
	*/

	fmt.Print("Enter the number > ")
	_, err := fmt.Scanln(&dIdx)
	if err != nil {
		log.Fatal(err)
	}
	division := divisions[dIdx]
	fmt.Println("Your division: ", division.name)
	fmt.Println("Your potential swaps: ", division.swaps)

	// Get the dates of the game swap
	// TODO validate the date format the user entered
	//      1. current or future date
	//      2. month and day values are valid
	var date string
	fmt.Print("Enter date (i.e. YYYY-MM-DD)")
	_, err = fmt.Scanln(&date)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("Your date: ", date)

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

	// create a reader to read all lines from CSV file
	reader := csv.NewReader(f)

	// list of records
	var records [][]string

	// used to store the list of team names
	var teamNames []string
	var teamsToEliminate []string

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

		// Skip any games that already have a score entered
		// Example: GLOUCESTER CENTRE COUGARS U15B1 (3)
		result, err := regexp.MatchString(`.*\([0-9]+\).*`, record[6])
		// TODO : add error handling
		if result {
			// fmt.Println("skipping: ", record)
			continue
		}

		// look for games in divisions that match swap rules
		result, err = regexp.MatchString(division.swapsRegex, record[0])
		// TODO : add error handling
		if result {
			//fmt.Println("match: ", record)
			records = append(records, record)
		}

		// look for teams in the same division
		result, err = regexp.MatchString(division.nameRegex, record[0])
		// TODO : add error handling
		if result {
			//fmt.Println("adding :", record[5])
			//fmt.Println("adding :", record[6])
			teamNames = addUnique(teamNames, record[5])
			teamNames = addUnique(teamNames, record[6])
		}

		// Create a list of all teams playing on the date to be swapped
		// These teams will be eliminated from potential matches
		result, err = regexp.MatchString(date, record[2])
		// TODO : add error handling
		if result {
			teamsToEliminate = addUnique(teamsToEliminate, record[5])
			teamsToEliminate = addUnique(teamsToEliminate, record[6])
		}
		cnt++
	}

	// prompt for team
	tIdx := 0
	for _, k := range teamNames {
		fmt.Printf(" %d - %s\n", tIdx, k)
		tIdx++
	}
	fmt.Print("Select team: ")
	_, err = fmt.Scanln(&tIdx)
	if err != nil {
		log.Fatal(err)
	}
	team := teamNames[tIdx]
	fmt.Print("Enter the number > ")
	fmt.Println("Your team: ", team)

	// todo - uncomment when I figure out what this is
	//var potentialGames[][]string

	// 1) find all teams (division + team name) playing on the given date
	// 2) remove your opponent from the list of teams
	// 3) eliminate all teams playing on the given date
	for i := range records {
		result, err := regexp.MatchString(date, records[i][2])
		if result {
			// todo This shouldn't be a fatal error
			log.Fatal(err)
		}
	}
}
