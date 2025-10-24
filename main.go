/*
  Package for finding game swaps.

  TODO Add graphical interface
  TODO Convert CSV to Excel file
  TODO Prompt for other teams to exclude (i.e. declined due to tournaments)
*/

package main

import (
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

	"github.com/GeoffreyPlitt/debuggo"
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

// Structure to hold swap information
type swap_t struct {
	date         string     // date of the game to swap
	gameId       string     // game id
	home         string     // teams needing a swap
	away         string     // teams needing a swap
	excludeTeams []string   // list of team already playing on swap date
	excludeDates []string   // list of dates swap game teams are playing on
	games        [][]string // list of potentialMatches from the schedule file
}

// Structure to hold information about divisions
type division_type struct {
	name       string // name of the division
	nameRegex  string // regex for matching division
	swaps      string // description of swaps
	swapsRegex string // regular expression for finding swaps
}

// Structure to hold TTM API response
type TTMResponse struct {
	ID   int    `json:"id"`
	Data string `json:"data"` // This field is a Base64 encoded JSON string
}

// Structure to hold TTM Schedule Records
// Used to unmarshal the decoded JSON data
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
   
// Structure to hold TTM API response for team contacts
type TTMContacts struct {
	ID           string `json:"id"`
	Division     string `json:"divisionName"`
	Category     string `json:"categoryName"`
	Team         string `json:"teamName"`
	Coach        string `json:"coachName"`
	CoachEmail   string `json:"coachEmail"`
	Manager      string `json:"managerName"`
	ManagerEmail string `json:"managerEmail"`
	Type         string `json:"type"`
}

// Global variables
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
Fetch team contact information from TTM
*/
func teamContacts() map[string]TTMContacts {
	url := "https://api.off-iceoffice.ca/ooAPI/v1/schedules/teams/?orgID=district9&id=GHA"

	// Get the data from the URL
	resp, err := http.Get(url)
	if err != nil {
		log.Fatalf("Error fetching data: %v", err)
	}
	defer resp.Body.Close()

	// Extract the response body
	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Fatalf("Error reading response body, %v", err)
	}

	var ttmResponse TTMResponse
	err = json.Unmarshal(bodyBytes, &ttmResponse)
	if err != nil {
		log.Fatalf("Error unmarshaling JSON, %v", err)
	}

	decodedBytes, err := base64.StdEncoding.DecodeString(ttmResponse.Data)
	if err != nil {
		log.Fatalf("Error decoding base64 data, %v", err)
	}

	jsonFile, err := os.Create("contacts.json")
	if err != nil {
		log.Fatalf("Error creating JSON file, %v", err)
	}
	defer jsonFile.Close()
	_, err = jsonFile.Write(decodedBytes)
	if err != nil {
		log.Fatalf("Error writing to JSON file, %v", err)
	}

	var contacts []TTMContacts
	err = json.Unmarshal(decodedBytes, &contacts)
	if err != nil {
		log.Fatalf("Error unmarshaling contacts JSON, %v", err)
	}

	contactMap := make(map[string]TTMContacts)

	// Write contact data to CSV
	for _, contact := range contacts {
		contactMap[contact.Team] = contact
	}

	return contactMap
}

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
	// create a debugger object
	var debug = debuggo.Debug("downloadSchedule")

	var url string = "https://api.off-iceoffice.ca/ooAPI/v1/schedules/" +
		"games/?orgID=1567976101-7023700001&option1=88&" +
		"option2=9999&option3=2"

	// Get the data
	debug("Downloading schedule from %s", url)
	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	// Read the response body
	debug("Extract base64 encoded data from response")
	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Fatalf("Error reading response body: %v", err)
	}

	var ttm_shell TTMResponse
	err = json.Unmarshal([]byte(bodyBytes), &ttm_shell)
	if err != nil {
		fmt.Println("Error unmarshalling TTM Response Struct:", err)
		return
	}

	// Convert the byte slice to a string if the body is expected to be a Base64 string
	base64EncodedString := string(ttm_shell.Data)

	// Decode the Base64 data
	debug("Decoding Base64 encoded data")
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
	debug("Creating file: %s", filepath)
	csvFile, err := os.Create(filepath)
	if err != nil {
		log.Fatal("Could not create CSV file:", err)
	}
	defer csvFile.Close()

	writer := csv.NewWriter(csvFile)
	defer writer.Flush()

	// Write header row
	debug("Writing schedule to CSV file")
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
	// create a debugger object
	var debug = debuggo.Debug("main")

	// Structure to hold swap information
	var swap swap_t

	// location to download schedule to
	schedule := "./schedule.csv"

	// Set the cut off date for games to be considered
	// This is today + 10 days
	// Any games on or before this date will be ignored
	cutOffDate := time.Now().AddDate(0, 0, 10)

	// Auto download the schedule
	if err := downloadSchedule(schedule); err != nil {
		log.Panic(err)
	}

	// open file for reading
	debug("Opening schedule file: %s", schedule)
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
	debug("Reading schedule file into memory")
	swap.games, err = reader.ReadAll()
	if err != nil {
		log.Fatal(err)
	}

	// Get the team contacts
	contacts := teamContacts()

	// Use the game id to find the division and teams needing a swap
	// This will be used to find the dates and teams to exclude
	// when searching for potential matches
	var division division_type
	for line, game := range swap.games {
		if game[GAMEID] == swap.gameId {
			// Game was found, extract the information
			debug("Found game %s on line %d\n", swap.gameId, line)
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
			debug(strings.Join(game, ","))
			return true
		}
		if gameDate.Before(cutOffDate) {
			// delete any games in the past or 7 days from today
			debug(strings.Join(game, ","), " << before cutoff date")
			return true
		}
		if !swappableRe.MatchString(game[DIVISION]) {
			// delete if can't swap with the division
			debug(strings.Join(game, ","), " << wrong division")
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
			debug(strings.Join(game, ","), " << swapping team")
		}

		// Get the names of all teams already playing on the day of the
		// swap game. All these teams can be dropped as potential matches
		if swap.date == game[DATE] {
			debug(strings.Join(game, ","), " << playing on swap date")
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

	// Open file to write possible game swaps to
	debug("Creating output file: %s", swap.gameId+".csv")
	csvFile, err := os.Create(swap.gameId + ".csv")
	if err != nil {
		log.Panic(err)
	}
	defer csvFile.Close()

	// Write CSV header
	writer := csv.NewWriter(csvFile)
	writer.Write([]string{"Division", "Game ID", "Date", "Time", "Arena", "Home Team", "Away Team", "Contacts"})
	writer.Flush()

	for _, g := range swap.games {
		

		fmt.Println(strings.Join(g, ","))
		csvFile.WriteString(strings.Join(g, ","))
		csvFile.WriteString(strings.Join([]string{",",
		                      contacts[swap.home].CoachEmail, 
			                  contacts[swap.home].ManagerEmail,
			                  contacts[swap.away].CoachEmail,
							  contacts[swap.away].ManagerEmail,
							  contacts[g[HOMETEAM]].CoachEmail,
							  contacts[g[HOMETEAM]].ManagerEmail,
							  contacts[g[AWAYTEAM]].CoachEmail,
							  contacts[g[AWAYTEAM]].ManagerEmail}, ";"))
		csvFile.WriteString("\n")
	}

	fmt.Printf("Recorded %d potential matches to %s\n", len(swap.games),
		swap.gameId+".csv")

	fmt.Println("Press enter to contine")
	fmt.Scanln()

}
