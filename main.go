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
	"bufio"
	"encoding/base64"
	"encoding/csv"
	"encoding/json"
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
	games        [][]string // list of potentialMatches from the schedule file
}
type division_type struct {
	name       string // name of the division
	nameRegex  string // regex for matching division
	swaps      string // description of swaps
	swapsRegex string // regular expression for finding swaps
}

type TTMResponseStruct struct {
	ID   int    `json:"id"`
	Data string `json:"data"` // This field contains a JSON string
}

type TTMScheduleRecord struct {
	ID       string `json:"id"`
	GameID   string `json:"gameID"`
	GameDate string `json:"gameDate"`
	GameTime string `json:"gameTime"`
	Venue    string `json:"venue"`
	Division string `json:"division"`
	HomeTeam string `json:"homeTeam"`
	AwayTeam string `json:"awayTeam"`
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
	var url string = "https://api.off-iceoffice.ca/ooAPI/v1/schedules/" +
		"games/?orgID=1567976101-7023700001&option1=88&" +
		"option2=9999&option3=2"

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

	// Read the response body
	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Fatalf("Error reading response body: %v", err)
	}

	var ttm_shell TTMResponseStruct
	err = json.Unmarshal([]byte(bodyBytes), &ttm_shell)
	if err != nil {
		fmt.Println("Error unmarshalling TTM Response Struct:", err)
		return
	}

	// Convert the byte slice to a string if the body is expected to be a Base64 string
	base64EncodedString := string(ttm_shell.Data)

	// Decode the Base64 data
	decodedBytes, err := base64.StdEncoding.DecodeString(base64EncodedString)
	if err != nil {
		log.Fatalf("Error decoding Base64 string: %v", err)
	}

	var scheduleRecords []TTMScheduleRecord
	err = json.Unmarshal(decodedBytes, &scheduleRecords)
	if err != nil {
		fmt.Println("Error decoding the schedule rows", err)
		return
	}

	// Write the 'scheduleRecords' variable, which is an array (slice) of structs, to file as a CSV.
	// We'll open a file for writing, create a csv.Writer, and write a header plus all games.
	csvFile, err := os.Create(filepath)
	if err != nil {
		log.Fatal("Could not create CSV file:", err)
	}
	defer csvFile.Close()

	writer := csv.NewWriter(csvFile)
	defer writer.Flush()

	// Write header row
	err = writer.Write([]string{"Division", "GameID", "Date", "Time", "Arena", "Home Team", "Away Team"})
	if err != nil {
		log.Fatal("Could not write CSV header:", err)
	}

	// Write each game as a CSV row
	for _, g := range scheduleRecords {
		err := writer.Write([]string{
			g.Division,
			g.GameID,
			g.GameDate,
			g.GameTime,
			g.Venue,
			g.HomeTeam,
			g.AwayTeam,
		})
		if err != nil {
			log.Fatal("Could not write game to CSV:", err)
		}
	}

	/*fmt.Println("Test")

	// Writer the body to file
	_, err = io.Copy(out, resp.Body)
	if err != nil {
		return err
	}*/

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

func promptWithDefault(prompt string, def string) string {
	reader := bufio.NewReader(os.Stdin)
	fmt.Printf("%s (press enter for default: '%s'): ", prompt, def)
	input, err := reader.ReadString('\n')
	if err != nil {
		log.Fatal(err)
	}
	input = strings.TrimSpace(input)
	if input == "" {
		input = def
	}
	return input
}

func main() {
	var swap swap_t // structure to track data for swaps
	/*
		// TODO prompt user for file name
		schedule := promptWithDefault("Input file", "./schedule.csv") // location to download schedule to

		// open file for reading
		fi, err := os.Open(schedule)
		if err != nil {
			log.Fatal(err)
		}
		defer fi.Close()
	*/
	schedule := "./schedule.csv" // location to download schedule to

	// Set the cut off date for games to be considered
	// This is today + 10 days
	// Any games on or before this date will be ignored
	cutOffDate := time.Now().AddDate(0, 0, 10)

	// Auto download the schedule
	if err := downloadSchedule(schedule); err != nil {
		log.Panic(err)
	}

	// open file for reading
	fi, err := os.Open(schedule)
	if err != nil {
		log.Fatal(err)
	}
	defer fi.Close()

	// Get the game id
	// This is use to find the two teams that are playing. Team names will be
	// used to find dates to exclude
	fmt.Print("Enter Id of game to swap (i.e. HLU1501): ")
	_, err = fmt.Scanln(&swap.gameId)
	if err != nil {
		log.Fatal(err)
	}
	// create a reader to read all lines from CSV file
	reader := csv.NewReader(fi)

	// Read all the records into memory
	swap.games, err = reader.ReadAll()
	if err != nil {
		log.Fatal(err)
	}
	//fmt.Printf("Lines: %d\n", len(swap.matches))

	// Use the game id to find the division and teams needing a swap
	// This will be used to find the dates and teams to exclude
	// when searching for potential matches
	var division division_type
	for _, game := range swap.games {
		if game[GAMEID] == swap.gameId {
			// Game was found, extract the information
			// TODO - delete debug statement
			// fmt.Printf("Found game %s on line %d\n", swap.gameId, i)
			swap.date = game[DATE]
			swap.home = game[HOMETEAM]
			swap.away = game[AWAYTEAM]
			fmt.Println("Game date: ", swap.date)
			fmt.Println("Home team: ", swap.home)
			fmt.Println("Away team: ", swap.away)

			// Selec the right division by matching the regex with the division
			// name from the game
			for _, division = range divisions {
				matched, err := regexp.MatchString(division.nameRegex, game[DIVISION])
				if err != nil {
					log.Fatal(err)
				}
				if matched {
					fmt.Println("Your division: ", division.name)
					fmt.Println("Searching for swaps with the following divisions: ", division.swaps)
					break
				}
			}

			// Check that the game date is not before the cut off date
			// If it is then there is no point in continuing
			gameDate, err := time.Parse(DATE_FORMAT, swap.date)
			if err != nil {
				log.Fatal(err)
			}
			if gameDate.Before(cutOffDate) {
				fmt.Println("Game date is before cut off date of ", cutOffDate.Format(DATE_FORMAT))
				fmt.Println("No point in continuing")
				return
			}

			// Exit the loop as the game has been found
			break
		}
	}
	// compile regex to check if division is acceptable for swaps
	swappableRe, err := regexp.Compile(division.swapsRegex)
	if err != nil {
		log.Fatal(err)
	}

	// Delete games that
	//  - occur in the past
	//  - don't match the swappable divisions
	swap.games = slices.DeleteFunc(swap.games, func(game []string) bool {
		gameDate, err := time.Parse(DATE_FORMAT, game[DATE])
		if err != nil {
			// probably here because the first line is a header
			//fmt.Println(game) // TODO - delete debug statement
			return true
		}
		if gameDate.Before(cutOffDate) {
			// delete any games in the past or 7 days from today
			//fmt.Println(game, " << before cutoff date") // TODO - delete debug statement
			return true
		}
		if !swappableRe.MatchString(game[DIVISION]) {
			// delete if can't swap with the division
			//fmt.Println(game, " << wrong division") // TODO - delete debug statement
			return true
		}
		return false
	})
	//fmt.Printf("Lines: %d\n", len(swap.matches))

	// Build lists of dates and teams to exclude from potential matches
	// 1. dates when the teams in the swaps are playing
	// 2. teams that are already playing on the swap date
	for _, game := range swap.games {
		if slices.Contains(game, swap.home) || slices.Contains(game, swap.away) {
			swap.excludeDates = append(swap.excludeDates, game[DATE])
			//fmt.Println(game, " << swapping team")
		}

		// Get the names of all teams already playing on the day of the
		// swap game. All these teams can be dropped as potential matches
		if swap.date == game[DATE] {
			fmt.Println(game, " << playing on swap date")
			swap.excludeTeams = addUnique(swap.excludeTeams, game[HOMETEAM])
			swap.excludeTeams = addUnique(swap.excludeTeams, game[AWAYTEAM])
		}
	}

	// Remove any games
	// 1. for dates where the teams needing a swap are playing
	// 2. involving other teams playing on the day of the swap
	swap.games = slices.DeleteFunc(swap.games, func(game []string) bool {
		if slices.Contains(swap.excludeDates, game[DATE]) {
			return true
		}
		if slices.Contains(swap.excludeTeams, game[HOMETEAM]) {
			return true
		}
		if slices.Contains(swap.excludeTeams, game[AWAYTEAM]) {
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

	for _, g := range swap.games {
		fmt.Println(strings.Join(g, ","))
		if _, e := fo.WriteString(strings.Join(g, ",") + "\n"); e != nil {
			log.Panic(e)
		}
	}

	fmt.Printf("Recorded %d potential matches to %s\n", len(swap.games),
		swap.gameId+".csv")

	fmt.Println("Press enter to contine")
	fmt.Scanln()

}
