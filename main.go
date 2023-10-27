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
		// U9
		{"U9 A", "U9.*A", "U9 A -> U9 A-C", "U9.*[A-C]"},
		{"U9 B", "U9.*B", "U9 B -> U9 A-C", "U9.*[A-C]"},
		{"U9 C", "U9.*C", "U9 C -> U9 A-C", "U9.*[A-C]"},
		// U11
		{"U11 A", "U11.*A", "U11 A -> U11 A-C, U13 B-C", "U11.*[A-C]|U13.*[B-C]"},
		{"U11 B", "U11.*B", "U11 B -> U11 A-C, U13 B-C", "U11.*[A-C]|U13.*[B-C]"},
		{"U11 C", "U11.*C", "U11 C -> U11 A-C, U13 B-C", "U11.*[A-C]|U13.*[B-C]"},
		// U13
		{"U13 A", "U13.*A", "U13 A -> U15 A-B", "U13.*[A]|U15.*[A-B]"},
		{"U13 B", "U13.*B", "U13 B -> U11 A-C, U13 B-C", "U13.*[B-C]|U11.*[A-C]"},
		{"U13 C", "U13.*C", "U13 C -> U11 A-C, U13 B-C", "U13.*[B-C]|U11.*[A-C]"},
		// U15
		{"U15 A", "U15.*A", "U15 A -> U13 A, U15 A-B, U18 A-B", "U13.*A|U15.*[A-B]|U18.*[A-B]"},
		{"U15 B", "U15.*B", "U15 B -> U13 A, U15 A-B, U18 A-B", "U13.*A|U15.*[A-B]|U18.*[A-B]"},
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
	schedule := "./schedule.csv" // location to download schedule to

	// Download schedule
	if err := downloadSchedule(schedule); err != nil {
		log.Panic(err)
	}

	// Open file to write possible game swaps to
	// TODO pull the file name from the game to be swapped
	swap_options := "./swaps.csv" // file containing possible swaps
	fo, err := os.Create(swap_options)
	if err != nil {
		log.Panic(err)
	}
	defer fo.Close()

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
	fmt.Println("Searching for swaps with the following divisions: \n  ", division.swaps)

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
	// debug statement
	//fmt.Println("Your date: ", date)

	// open file for reading
	fi, err := os.Open(schedule)
	if err != nil {
		log.Fatal(err)
	}
	defer fi.Close()

	// create a reader to read all lines from CSV file
	reader := csv.NewReader(fi)

	// list of potentialMatches from the schedule file
	var potentialMatches [][]string

	// Constants used to access gameInfo records in the CSV
	const (
		DIVISION   = 0
		GAMEID     = 1
		DATE       = 2
		TIME       = 3
		VENUE      = 4
		HOMETEAM   = 5
		AWAYTEAM   = 6
		GAMESTATUS = 7
	)

	// used to store the list of team names
	var teamNames []string // Need? a list to present options to users
	var teamSchedule = make(map[string]map[string]bool)
	var teamsToEliminate = make(map[string]bool) // Map is better to do fast lookups and avoid iterating over a list

	// compile regex to check if scores entered
	// TODO check error code
	skipRe, err := regexp.Compile(`.*\([0-9]+\).*`)
	if err != nil {
		log.Fatal(err)
	}

	// compile regex to check if division is acceptable for swaps
	// TODO check error code
	divRe, err := regexp.Compile(division.nameRegex)
	if err != nil {
		log.Fatal(err)
	}

	for line := 1; ; line++ {
		// read each gameInfo from the file
		gameInfo, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			log.Fatal(err)
		}

		// Skip any games that already have a score entered
		// Example: GLOUCESTER CENTRE COUGARS U15B1 (3)
		result, err := regexp.MatchString(`.*\([0-9]+\).*`, record[6])
		if err != nil {
			log.Fatal(err)
		}
		if result {
			//log.Println(line, "skipping: ", record)
			continue
		}

		// Create a list of teams in the same division. This will be used
		// later to prompt the user what their team is. Done before skipping
		// games with scores because early season schedules may not have
		// unplayed games for all teams. This maximizes the chances that all
		// teams will be found.
		result, err := regexp.MatchString(division.nameRegex, gameInfo[DIVISION])
		if err != nil {
			log.Fatal(err)
		}
		if result {
			//fmt.Println(line, "add division team :", record[5])
			//fmt.Println(line, "add division team :", record[6])
			for i := 5; i <= 6; i++ {
				teamNames = addUnique(teamNames, record[i])
				if teamSchedule[record[i]] == nil {
					teamSchedule[record[i]] = make(map[string]bool)
				}
				teamSchedule[gameInfo[i]][gameInfo[GAMEID]] = true
			}
		}

		// look for games in divisions that satisfy the swap rules
		result, err = regexp.MatchString(division.swapsRegex, record[0])
		if err != nil {
			log.Fatal(err)
		}
		if result {
			//fmt.Println(line, "match: ", record)
			potentialMatches = append(potentialMatches, gameInfo)
		}

		// Create a list of all teams playing on the date to be swapped
		// These teams will be eliminated from potential matches
		result, err = regexp.MatchString(date, gameInfo[DATE])
		if err != nil {
			log.Fatal(err)
		}
		if result {
			//fmt.Println(line, "eliminate team: ", record[5])
			//fmt.Println(line, "eliminate team: ", record[6])
			teamsToEliminate[gameInfo[HOMETEAM]] = true
			teamsToEliminate[gameInfo[AWAYTEAM]] = true
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
	fmt.Println("Your team: ", team)

	// debug statement
	//fmt.Println("Schedule:", teamSchedule[team])
	var matches [][]string

	// TODO eliminate dates when you are already playing

	// 1) find all teams (division + team name) playing on the given date
	// 2) remove your opponent from the list of teams
	// 3) eliminate all teams playing on the given date
	// 4) eliminate all dates that the team is playing on
	for i := range potentialMatches {
		result, err := regexp.MatchString(date, potentialMatches[i][2])
		if err != nil {
			log.Fatal(err)
		}
		if result {
			// skip games on the same day
			continue
		}

		if teamSchedule[team][potentialMatches[i][2]] {
			// skip days when the team is already playing
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
		fmt.Println(strings.Join(potentialMatches[i], ","))
		if _, err := fo.WriteString(strings.Join(potentialMatches[i], ",") + "\n"); err != nil {
			log.Panic(err)
		}

		matches = append(matches, potentialMatches[i])
	}
}
