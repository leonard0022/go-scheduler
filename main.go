/*
  Package for finding game swaps.

  TODO Find out how to have a debug flag and debug statements
  TODO Add graphical interface
  TODO Add header to output CSV file
  TODO Convert CSV to Excel file
  TODO Move to Google cloud
  TODO Prompt for other teams to exclude (i.e. declined due to tournaments)
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
	"slices"
	"strings"
	"time"
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

type swap_t struct {
	date         string     // date of the game to swap
	gameId       string     // game id
	home         string     // teams needing a swap
	away         string     // teams needing a swap
	excludeTeams []string   // list of team already playing on swap date
	excludeDates []string   // list of dates swap game teams are playing on
	matches      [][]string // list of potentialMatches from the schedule file
}
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

// Constants used to access gameInfo records in the CSV
const (
	DATE_FORMAT = "2006-01-02"
	DIVISION    = 0
	GAMEID      = 1
	DATE        = 2
	TIME        = 3
	VENUE       = 4
	HOMETEAM    = 5
	AWAYTEAM    = 6
	GAMESTATUS  = 7
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
	var swap swap_t // structure to track data for swaps

	schedule := "./schedule.csv" // location to download schedule to

	cutOffDate := time.Now().AddDate(0, 0, 10)

	// Download schedule
	if err := downloadSchedule(schedule); err != nil {
		log.Panic(err)
	}

	// Get team division from user
	// TODO extract from schedule based on GameId to swap
	fmt.Println("Select your division: ")
	dIdx := 0
	for _, k := range divisions {
		fmt.Printf(" %2d - %s\n", dIdx, k.name)
		dIdx++
	}
	fmt.Print("Enter the number > ")
	_, err := fmt.Scanln(&dIdx)
	if err != nil {
		log.Fatal(err)
	}
	division := divisions[dIdx]
	fmt.Println("Your division: ", division.name)
	fmt.Println("Searching for swaps with the following divisions: \n  ", division.swaps)

	// Get the game id
	// This is use to find the two teams that are playing. Team names will be
	// used to find dates to exclude
	fmt.Print("Enter Id of game to swap (i.e. HLU1501): ")
	_, err = fmt.Scanln(&swap.gameId)
	if err != nil {
		log.Fatal(err)
	}

	// open file for reading
	fi, err := os.Open(schedule)
	if err != nil {
		log.Fatal(err)
	}
	defer fi.Close()

	// create a reader to read all lines from CSV file
	reader := csv.NewReader(fi)

	// compile regex to check if division is acceptable for swaps
	swappableRe, err := regexp.Compile(division.swapsRegex)
	if err != nil {
		log.Fatal(err)
	}

	// Read all the records into memory
	swap.matches, err = reader.ReadAll()
	if err != nil {
		log.Fatal(err)
	}
	//fmt.Printf("Lines: %d\n", len(swap.matches))

	// Delete games that
	//  - occur in the past
	//  - don't match the swappable divisions
	swap.matches = slices.DeleteFunc(swap.matches, func(g []string) bool {
		gameDate, err := time.Parse(DATE_FORMAT, g[DATE])
		if err != nil {
			// probably here because the first line is a header
			//fmt.Println(g) // TODO - delete debug statement
			return true
		}
		if gameDate.Before(cutOffDate) {
			// delete any games in the past or 7 days from today
			//fmt.Println(g) // TODO - delete debug statement
			return true
		}
		if !swappableRe.MatchString(g[DIVISION]) {
			// delete if can't swap with the division
			//fmt.Println(g) // TODO - delete debug statement
			return true
		}
		return false
	})
	//fmt.Printf("Lines: %d\n", len(swap.matches))

	// Parse the slice for information about teams in your division
	for _, g := range swap.matches {
		// Get info about game swap
		// 1. teams involved in swap - eliminate dates this teams are playing
		// 2. date of swap - eliminate teams already playing this day
		if swap.gameId == g[GAMEID] {
			fmt.Println(g[HOMETEAM])
			fmt.Println(g[AWAYTEAM])
			swap.home = g[HOMETEAM]
			swap.away = g[AWAYTEAM]
			swap.date = g[DATE]
			break
		}
	}

	// Build lists of dates and teams to exclude from potential matches
	// 1. dates when the teams in the swaps are playing
	// 2. teams that are already playing on the swap date
	for _, g := range swap.matches {
		if slices.Contains(g, swap.home) || slices.Contains(g, swap.away) {
			swap.excludeDates = append(swap.excludeDates, g[DATE])
		}

		// Get the names of all teams already playing on the day of the
		// swap game. All these teams can be dropped as potential matches
		if swap.date == g[DATE] {
			swap.excludeTeams = addUnique(swap.excludeTeams, g[HOMETEAM])
			swap.excludeTeams = addUnique(swap.excludeTeams, g[AWAYTEAM])
		}
	}

	// Remove any games
	// 1. for dates where the teams needing a swap are playing
	// 2. involving other teams playing on the day of the swap
	swap.matches = slices.DeleteFunc(swap.matches, func(g []string) bool {
		if slices.Contains(swap.excludeDates, g[DATE]) {
			return true
		}
		if slices.Contains(swap.excludeTeams, g[HOMETEAM]) {
			return true
		}
		if slices.Contains(swap.excludeTeams, g[AWAYTEAM]) {
			return true
		}
		return false
	})
	//fmt.Printf("Lines: %d\n", len(swap.matches))

	// Open file to write possible game swaps to
	fo, err := os.Create(swap.gameId + ".csv")
	if err != nil {
		log.Panic(err)
	}
	defer fo.Close()

	for _, g := range swap.matches {
		fmt.Println(strings.Join(g, ","))
		if _, e := fo.WriteString(strings.Join(g, ",") + "\n"); e != nil {
			log.Panic(e)
		}
	}

	fmt.Printf("Recorded %d potential matches to %s\n", len(swap.matches),
		swap.gameId+".csv")

	fmt.Println("Press enter to contine")
	fmt.Scanln()

}
