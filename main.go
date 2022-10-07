/*
  Package for finding game swaps.

  TODO Find out how to have a debug flag and debug statements
  TODO Add graphical interface
*/

package main

import (
	"encoding/csv"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"regexp"
	"sort"
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
		// U11
		{"U11 A", "U11.*A", "U11 A -> U11 A-C, U13 B-C", "U11.*[A-C]|U13.*[B-C]"},
		{"U11 B", "U11.*B", "U11 B -> U11 A-C, U13 B-C", "U11.*[A-C]|U13.*[B-C]"},
		{"U11 C", "U11.*C", "U11 C -> U11 A-C, U13 B-C", "U11.*[A-C]|U13.*[B-C]"},
		// U13
		{"U13 A", "U13.*A", "U13 A -> U15 A-B", "U13.*[A]|U15.*[A-B]"},
		{"U13 B", "U13.*B", "U13 B -> U11 A-C, U13 B-C", "U13.*[B-C]|U11.*[A-C]"},
		{"U13 C", "U13.*C", "U13 C -> U11 A-C, U13 B-C", "U13.*[B-C]|U11.*[A-C]"},
		// U15
		{"U15 A", "U15.*A", "U15 A -> U15 A-B, U18 A-B", "U15.*[A-B]|U18.*[A-B]"},
		{"U15 B", "U15.*B", "U15 B -> U15 A-B, U18 A-B", "U15.*[A-B]|U18.*[A-B]"},
		// U18
		{"U18 A", "U18.*A", "U18 A -> U15 A-B, U18 A-B", "U15.*[A-B]|U18.*[A-B]"},
		{"U18 B", "U18.*B", "U18 B -> U15 A-B, U18 A-B", "U15.*[A-B]|U18.*[A-B]"},
	}
)

/*
Download GHA Schedule to local

This is used to download the schedule from the Total Team Management
website. To get the URL (Note: done with Firefox)
 1. Navigate to the TTM website schedules
 2. Select 'All Divisions'
 3. Enable Developer Tools: Ctrl + Shift + I
 4. In Developer Tools, select Network tab
 5. Click the TTM Export... button and choose CSV format
 6. Close the popup window
 7. In Developer Tools right click the new File value
 8. Select Copy Value / Copy URL
*/
func downloadSchedule(filepath string) (err error) {
	// url to the full schedule
	var url string = "https://ttmwebservices.ca/schedules/index.php?" +
		"pgid=dnl-11-010&dtype=CSV&AID=HEO&JID=district9&" +
		"pcode=15679761017023700001&ddtype=&stype=2&atype="

	// Create the file
	out, err := os.Create(filepath)
	if err != nil {
		return err
	}
	defer out.Close()

	// Get the data
	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	// Writer the body to file
	_, err = io.Copy(out, resp.Body)
	if err != nil {
		return err
	}

	return nil
}

/*
Normalize everything to uppercase. Check to see if the string is already in
the list. If so then return the original list; otherwise, append the new
string and return the updated list.
*/
func addUnique(list []string, str string) []string {
	// If scores have been added you need to cut the scores
	// Example:  BLACKBURN STINGERS U15 B1 (1) -> BLACKBURN STINGERS U15 B1
	before, _, _ := strings.Cut(str, " (")
	tStr := strings.ToUpper(before)

	//list[tStr] = true
	for _, v := range list {
		if strings.ToUpper(v) == tStr {
			return list
		}
	}

	list = append(list, tStr)
	return list
}

func main() {
	// location to download schedule to
	schedule := "./schedule.csv"

	// Download schedule
	// TODO clean up downloaded file when done
	err := downloadSchedule(schedule)
	if err != nil {
		log.Fatal(err)
	}

	// Get team division from user
	fmt.Println("Select your division: ")
	dIdx := 0
	for _, k := range divisions {
		fmt.Printf(" %2d - %s\n", dIdx, k.name)
		dIdx++
	}

	fmt.Print("Enter the number > ")
	_, err = fmt.Scanln(&dIdx)
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
	fmt.Print("Enter date to swap (i.e. YYYY-MM-DD): ")
	_, err = fmt.Scanln(&date)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("Your date: ", date)

	// open file for reading
	f, err := os.Open(schedule)
	if err != nil {
		log.Fatal(err)
	}
	defer f.Close()

	// create a reader to read all lines from CSV file
	reader := csv.NewReader(f)

	// list of potentialMatches from the schedule file
	var potentialMatches [][]string

	// used to store the list of team names
	var teamNames []string                       // Need? a list to present options to users
	var teamsToEliminate = make(map[string]bool) // Map is better to do fast lookups and avoid iterating over a list

	for line := 1; ; line++ {
		// read each record from the file
		record, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			log.Fatal(err)
		}

		// Create a list of teams in the same division. This will be used
		// later to prompt the user what their team is. Done before skipping
		// games with scores because early season schedules may not have
		// unplayed games for all teams. This maximizes the chances that all
		// teams will be found.
		result, err := regexp.MatchString(division.nameRegex, record[0])
		if err != nil {
			log.Fatal(err)
		}
		if result {
			//fmt.Println(line, "add division team :", record[5])
			//fmt.Println(line, "add division team :", record[6])
			teamNames = addUnique(teamNames, record[5])
			teamNames = addUnique(teamNames, record[6])
		}

		// Skip any games that already have a score entered
		// Example: GLOUCESTER CENTRE COUGARS U15B1 (3)
		result, err = regexp.MatchString(`.*\([0-9]+\).*`, record[6])
		if err != nil {
			log.Fatal(err)
		}
		if result {
			//log.Println(line, "skipping: ", record)
			continue
		}

		// look for games in divisions that satisfy the swap rules
		result, err = regexp.MatchString(division.swapsRegex, record[0])
		if err != nil {
			log.Fatal(err)
		}
		if result {
			//fmt.Println(line, "match: ", record)
			potentialMatches = append(potentialMatches, record)
		}

		// Create a list of all teams playing on the date to be swapped
		// These teams will be eliminated from potential matches
		result, err = regexp.MatchString(date, record[2])
		if err != nil {
			log.Fatal(err)
		}
		if result {
			fmt.Println(line, "eliminate team: ", record[5])
			fmt.Println(line, "eliminate team: ", record[6])
			teamsToEliminate[record[5]] = true
			teamsToEliminate[record[6]] = true
		}
	}

	// prompt for the user's team
	sort.Strings(teamNames)
	tIdx := 0
	for _, k := range teamNames {
		fmt.Printf(" %2d - %s\n", tIdx, k)
		tIdx++
	}
	fmt.Print("Select your team: ")
	_, err = fmt.Scanln(&tIdx)
	if err != nil {
		log.Fatal(err)
	}
	team := teamNames[tIdx]
	fmt.Print("Enter the number > ")
	fmt.Println("Your team: ", team)

	var matches [][]string

	// 1) find all teams (division + team name) playing on the given date
	// 2) remove your opponent from the list of teams
	// 3) eliminate all teams playing on the given date
	for i := range potentialMatches {
		result, err := regexp.MatchString(date, potentialMatches[i][2])
		if err != nil {
			log.Fatal(err)
		}
		if result {
			// skip games on the same day
			continue
		}

		if teamsToEliminate[potentialMatches[i][5]] {
			//fmt.Println("Eliminate: ", potentialMatches[i])
			continue
		}
		if teamsToEliminate[potentialMatches[i][6]] {
			//fmt.Println("Eliminate: ", potentialMatches[i])
			continue
		}

		// No reason found to eliminate the game, add it to matches
		fmt.Println("Match: ", potentialMatches[i])
		matches = append(matches, potentialMatches[i])
	}
}
