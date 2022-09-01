package main
// starapi - basic purpose is to monitor api port and when asked, to check star DB on hive 
//           if there is an unreconciled session entered into rec table
//
// Meant to compiled on windows using:
//   C:\Users\robej\go\src\starapi>set GOOS=linux
//   C:\Users\robej\go\src\starapi>go build -o starapi  
// Then uploaded to the HIVE account starapi, into the folder /home/starapi/go/bin using filemanager
// You must right click on the file and set permission to 744
// Execute in the cpanel terminal after cd /home/starapi/go/bin, using command   ./starapi
//         to make output and errors to log file   ./starapi &>> starapi.log
//         to run in background                    ./starapi &>> starapi.log &
//             This will show you a process ID like 22530
//             jobs     will return what is running
//             ps -h    will show you process IDs running - look for ./starapi
//             kill 22530     will terminate process - or use WHM
//
// Will persist listening to port 1962, even when cpanel closed
// Note: On WHM
//   To monitor port being used:   Home »Plugins »ConfigServer Security & Firewall  
//   To see and kill a process from starapi (after terminal closed) use:    Home »System Health »Process Manager
//
//////////////////////////
//
// Three Real Commands:
// NOTE - all 3 commends need the dev OR sys specified
//      - dev = ASO TEST
//      - sys = ASO PRODUCTION
//
//    http://starapi.cbhc.systems:1962/unrec/sys
//        If no record found (RecDateTime IS NULL), it sends back     {"RecID":"0","SessionID":""}
//        If found then returns something like                        {"RecID":"2","SessionID":"ASOCKS19073101"}
//
//    http://starapi.cbhc.systems:1962/success/sys/1
//         Marks a rec record with the ID passed as Reconciled, sends   {"Command":"success","Status":"OK"}
//               OR returns an error like NOTINDB, WASNOTPROCESSING
//
//    http://starapi.cbhc.systems:1962/recerror/sys/1
//         Marks a rec record with the ID passed as Error, sends   {"Command":"recerror","Status":"OK"}
//
// NOT USED ANYMORE
//
//    http://starapi.cbhc.systems:1962/reset
//         Does a reset of all records in rec table for testing, sends   {"Command":"reset","Status":"OK"}
//
//
// NEW COMMAND - to work around the block on new NetAtWork Cloud server
//
//    http://starapi.cbhc.systems:1962/askmipver
//        It checks MIP version on NetAtWork cloud server inhouse MIP: sends back     {"version":"19.2.0.0"}
//        It is essentially triggering starapi to run the familiar call to MIP api on NetAtWork
//			http://216.58.162.156:1962/api/security/version
//
//
//////////////////////
//
// NOTE: To install an external library from github
//       Open Cisco AnyConnect Client in lower right
//       Under network, change from wired to mobility
//       From terminal this works:dir
//       go get -u github.com/go-sql-driver/mysql
//

import (
	"bytes"
    "encoding/json"
	"github.com/tidwall/gjson"
    "fmt"
    "log"
    "net/http"
    "io/ioutil"
    "github.com/gorilla/mux"
	"github.com/gorilla/handlers"
	
	"database/sql"
	_ "github.com/go-sql-driver/mysql"
	"strings"
	"strconv"
	"time"
	"flag"
	"net"
	
	"reflect"
)



// this is what starapi returns if an unreconciled payinvoice session is found in STAR DB table
type RecRecord struct {
    RecID string `json:"RecID"`
    SessionID string `json:"SessionID"`
}

// from DB
type RecDB struct {
	RecID   int
	SessionID  string
//	Notes   string
//	UserIDUpdated  int
//	LoginUpdated   string
//	DateTimeUpdated
//	RecDateTime
//	RecStatus  string
//	RecResults	string
}


var base_url string  // which server to use for MIP API calls
var token string     // used as global for signing in and getting a token and using it later
var version string = "1701.1"
var torso string // used when passing response back to star seeker



type JSONString string

// M is an alias for map[string]interface{}  used in rec process - to make an array of a map of string values
	type M map[string]interface{}
	
	
	
	
func get_mipversion() {
	// pulled over from starseeker on 11-6-2019

    url := base_url + "/api/security/version"
    //url := base_url + "/api/security/organizations"

    fmt.Println("Starting the api test application - with url = ", url)

    // response, err := http.Get("https://pokeapi.co/api/v2/")
    // response, err := http.Get("http://nova02.hcbocc.ad:9001/api/security/organizations")



    res, err := http.Get(url)
    if err != nil {
        // this happens if port is not working, etc

        //       fmt.Printf("The HTTP request failed with error %s\n", err)
        log.Fatal(err)  // this does work when the IP is blocked  ... connection refused

    } else {

        defer res.Body.Close()  // must do this

        fmt.Printf("HTTP: %s\n", res.Status)  // aka HTTP: 200 OK

        body, readErr := ioutil.ReadAll(res.Body)
	if readErr != nil {
		log.Fatal(readErr)
	} else {


            ///  fmt.Println(string(body))

 
           log.Println(string([]byte(body)))

           var data interface{} // TopTracks
           err = json.Unmarshal(body, &data)
           if err != nil {
               panic(err.Error())
           }
           fmt.Printf("Results: %v\n", data)




           //os.Exit(0)

        }
    }


}



func (j JSONString) MarshalJSON() ([]byte, error) {
    return []byte(j), nil
}



func homePage(w http.ResponseWriter, r *http.Request){
 
	
	currentTime := time.Now()	
    fmt.Println("Endpoint Hit: homePage on ", currentTime.Format("2006-01-02 15:04:05"))	
	
	
	////
	// get client IP address
	  // get client ip address
	  ip,_,_ := net.SplitHostPort(r.RemoteAddr)

	  // print out the ip address
	  fmt.Println(" - from IP Address: " + ip ) //+ "\n\n")

	  // sometimes, the user acccess the web server via a proxy or load balancer.
	  // The above IP address will be the IP address of the proxy or load balancer and not the user's machine.

	  // let's get the request HTTP header "X-Forwarded-For (XFF)"
	  // if the value returned is not null, then this is the real IP address of the user.
	  fmt.Println(" - X-Forwarded-For :" + r.Header.Get("X-FORWARDED-FOR"));
	//
	////	
	
	fmt.Fprintf(w, "<h1>Welcome to the starapi HomePage!</h1>")	
	fmt.Fprintf(w, currentTime.Format("2006-01-02 15:04:05"))
}



func returnOneUnRecSession(w http.ResponseWriter, r *http.Request){
	// looks for any RecDateTime not set to a date in the rec table
	// and then returns the RecID and SessionID 
	// will return a 0 and "" if not found

    vars := mux.Vars(r)
    starsystem := vars["starsystem"] // dev OR sys



	
	// connect to DB
	db := dbConn(starsystem)


	currentTime := time.Now()	
    fmt.Println("Endpoint Hit: returnOneUnRecSession on ", currentTime.Format("2006-01-02 15:04:05"), " for starsystem=",starsystem)
	
	
	////
	// get client IP address
	  // get client ip address
	  ip,_,_ := net.SplitHostPort(r.RemoteAddr)

	  // print out the ip address
	  fmt.Println(" - from IP Address: " + ip ) //+ "\n\n")

	  // sometimes, the user acccess the web server via a proxy or load balancer.
	  // The above IP address will be the IP address of the proxy or load balancer and not the user's machine.

	  // let's get the request HTTP header "X-Forwarded-For (XFF)"
	  // if the value returned is not null, then this is the real IP address of the user.
	  fmt.Println(" - X-Forwarded-For :" + r.Header.Get("X-FORWARDED-FOR"));
	//
	////


	
	
	
	///var s string
	
    //selrecDB, err := db.Query("SELECT RecID, SessionID FROM rec WHERE 1 = 1 ORDER BY RecID DESC LIMIT 1")
    //selrecDB, err := db.Query("SELECT RecID, SessionID FROM rec WHERE UNIX_TIMESTAMP(`RecDateTime`) IS NULL ORDER BY RecID ASC //LIMIT 1")
	selrecDB, err := db.Query("SELECT RecID, SessionID FROM rec WHERE UNIX_TIMESTAMP(`RecDateTime`) IS NULL AND RecStatus = 'Ready' ORDER BY RecID ASC LIMIT 1")
    if err != nil {
        panic(err.Error())
    }
    onerec := RecDB{}
    //res := []RecDB{}
    for selrecDB.Next() {
		var RecID   int
		var SessionID  string
        err = selrecDB.Scan(&RecID, &SessionID)
        if err != nil {
            panic(err.Error())
        }
        onerec.RecID = RecID
        onerec.SessionID = SessionID

		
		// Create JSON from the instance data.
		// ... Ignore errors.
		///b, _ := json.Marshal(onerec)
		// Convert bytes to string.
		///s = string(b)
		///fmt.Println("Here")		
		
    }	


	
	// look at STAR DB rec table for any rec with recDateTime == nil LIMIT 1
	// convert int to string first below
	unrecid := strconv.Itoa(onerec.RecID) //"1"                          // rec_ID value for unreconciled record in rec table in DB
	unrecsessionid := onerec.SessionID //"ASOCKSTEST19072602"  // sessionid value for unreconciled payinvoice session


	// mark the rec as done, by putting in the datetime into RecDateTime, so will not get returned again to api
	if unrecid != "0" {
		// only do it if one was found
		insForm, err := db.Prepare("UPDATE rec SET RecDateTime = NOW(), RecStatus = 'Processing'  WHERE RecID = ?")
		if err != nil {
			panic(err.Error())
		}
		insForm.Exec(unrecid)
		fmt.Println(" - Updated the RecDateTime to NOW for RecID = ",unrecid )
	}

	
	// close DB
	defer db.Close()
	
	
	
    //Allow CORS here By * or specific origin
    //w.Header().Set("Access-Control-Allow-Origin", "*")
	// return "OKOK"
    //w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
  //w.Header().Set("Content-Type", "application/json")
  //w.Header().Set("Access-Control-Allow-Origin", "*")
  //w.Header().Set("Access-Control-Allow-Headers","Content-Type,access-control-allow-origin, access-control-allow-headers")



	
	// encode the 2 values as json, uses 0 0 if none are found
	RecRec := RecRecord{RecID: unrecid, SessionID: unrecsessionid}
    json.NewEncoder(w).Encode(RecRec)

}

/*
func resetAllRec(w http.ResponseWriter, r *http.Request){
	// reset all rec records to RecDateTime = NULL for testing use only

	// setting tis type so I can pass the result of this back to the caller
	type CommandStatus struct {
		Command string `json:"Command"`
		Status string `json:"Status"`
	}
	
	// connect to DB
	db := dbConn(starsystem)
		
	currentTime := time.Now()	
    fmt.Println("Endpoint Hit: resetAllRec on ", currentTime.Format("2006-01-02 15:04:05"))
	

	////
	// get client IP address
	  // get client ip address
	  ip,_,_ := net.SplitHostPort(r.RemoteAddr)

	  // print out the ip address
	  fmt.Println(" - from IP Address: " + ip ) //+ "\n\n")

	  // sometimes, the user acccess the web server via a proxy or load balancer.
	  // The above IP address will be the IP address of the proxy or load balancer and not the user's machine.

	  // let's get the request HTTP header "X-Forwarded-For (XFF)"
	  // if the value returned is not null, then this is the real IP address of the user.
	  fmt.Println(" - X-Forwarded-For :" + r.Header.Get("X-FORWARDED-FOR"));
	//
	////
	
	
	// reset the rec table to all RecDateTime = NULL
	upForm, err := db.Prepare("UPDATE rec SET `RecDateTime` = NULL")
	if err != nil {
		panic(err.Error())
	}
	upForm.Exec()
	fmt.Println(" - Updated the RecDateTime to NULL for all records in rec")
	

	
	// close DB
	defer db.Close()

	
    //Allow CORS here By * or specific origin
    //w.Header().Set("Access-Control-Allow-Origin", "*")
	// return "OKOK"
    //w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
  //w.Header().Set("Content-Type", "application/json")
  //w.Header().Set("Access-Control-Allow-Origin", "*")
  //w.Header().Set("Access-Control-Allow-Headers","Content-Type,access-control-allow-origin, access-control-allow-headers")


	
	// encode the a response to send back as json
	///MyStringreset := "reset"   
	///MyStringOK := "OK"
	// encode the 2 values as json, uses 0 0 if none are found
	////MyCommandStatus := CommandStatus{Command: MyStringreset, Status: MyStringOK}
	
	s := `{"Command":"reset","Status":"OK"}`
	
	//MyCommandStatus := JSONString(s)
	
    ////content, _ := json.Marshal(JSONString(s))
	
    //json.NewEncoder(w).Encode(MyCommandStatus)
 //   json.NewEncoder(w).Encode(s)

	w.Write([]byte(s))	

}
*/


func healthCheck(w http.ResponseWriter, r *http.Request) {
    json.NewEncoder(w).Encode("Still alive!")
	
	currentTime := time.Now()	
    fmt.Println("Endpoint Hit: healthCheck on ", currentTime.Format("2006-01-02 15:04:05"))
	
	////
	// get client IP address
	  // get client ip address
	  ip,_,_ := net.SplitHostPort(r.RemoteAddr)

	  // print out the ip address
	  fmt.Println(" - from IP Address: " + ip ) //+ "\n\n")

	  // sometimes, the user acccess the web server via a proxy or load balancer.
	  // The above IP address will be the IP address of the proxy or load balancer and not the user's machine.

	  // let's get the request HTTP header "X-Forwarded-For (XFF)"
	  // if the value returned is not null, then this is the real IP address of the user.
	  fmt.Println(" - X-Forwarded-For :" + r.Header.Get("X-FORWARDED-FOR"));
	//
	////	
	
	
}


// This function put in before NetAtWork cutover 08-6-2019
func askmipverCheck(w http.ResponseWriter, r *http.Request) {



	// pulled over from starseeker on 11-6-2019

    //url := base_url + "/api/security/version"
    url := base_url + "/api/security/organizations"

    fmt.Println("Starting the api test application - with url = ", url)

    // response, err := http.Get("https://pokeapi.co/api/v2/")
    // response, err := http.Get("http://nova02.hcbocc.ad:9001/api/security/organizations")



    res, err := http.Get(url)
    if err != nil {
        // this happens if port is not working, etc

        //       fmt.Printf("The HTTP request failed with error %s\n", err)
        log.Fatal(err)  // this does work when the IP is blocked  ... connection refused

    } else {

        defer res.Body.Close()  // must do this

        fmt.Printf("HTTP: %s\n", res.Status)  // aka HTTP: 200 OK

        body, readErr := ioutil.ReadAll(res.Body)
		if readErr != nil {
			log.Fatal(readErr)
		} else {


				///  fmt.Println(string(body))

			   //torso = string([]byte(body))  // to use it below
			   torso = string(body)
			   ////torso = body  // to use it below
			  log.Println("~~~~~~~~~")
			  log.Println(torso)
			   
			   
			   log.Println(string([]byte(body)))

			   var data interface{} // TopTracks
			   err = json.Unmarshal(body, &data)
			   if err != nil {
				   panic(err.Error())
			   }
			   fmt.Printf("Results: %v\n", data)




			   //os.Exit(0)

        }
    }


    //json.NewEncoder(w).Encode("askmipver-reply")
    //json.NewEncoder(w).Encode(res.Status) // this worked with "200 OK"
    json.NewEncoder(w).Encode(torso)
	
	//w.Header().Set(
	
	
	currentTime := time.Now()	
    fmt.Println("Endpoint Hit: askmipverCheck on ", currentTime.Format("2006-01-02 15:04:05"))
	
	////
	// get client IP address
	  // get client ip address
	  ip,_,_ := net.SplitHostPort(r.RemoteAddr)

	  // print out the ip address
	  fmt.Println(" - from IP Address: " + ip ) //+ "\n\n")

	  // sometimes, the user acccess the web server via a proxy or load balancer.
	  // The above IP address will be the IP address of the proxy or load balancer and not the user's machine.

	  // let's get the request HTTP header "X-Forwarded-For (XFF)"
	  // if the value returned is not null, then this is the real IP address of the user.
	  fmt.Println(" - X-Forwarded-For :" + r.Header.Get("X-FORWARDED-FOR"));
	//
	////	
	
	
		println("----------------------------------------------")
		/////////get_mipversion() // old simple call test 	
		println("----------------------------------------------")
	
	
	
}



///////////////////////////////////////////////////////////////////////////




func authenticate_get_token(raw bool, dry bool) {

		///$.ajax({
		///	 method: "POST",
		///	 url:"http://172.26.72.108:9001/api/security/login", 
		///	 data:{
		///		login: "admin", 
		///		password: "cbhc2884", 
		///		org: "CBHC"
		///}, 
//1738
	fmt.Println("Getting my token at url")

	url := base_url + "/api/security/login"

    jsonData := map[string]string{"login": "admin", "password": "cbhC2884$", "org": "CBHC"}
    jsonValue, _ := json.Marshal(jsonData)


    // Create a new request using http
    //req, err := http.NewRequest("POST", url, nil)

    //req.SetBasicAuth("admin", "cbhc2884", "CBHC")

    // Send req using http Client
    ///client := &http.Client{}
    ///resp, err := http.Post(req)

    resp, err := http.Post(url, "application/json", bytes.NewBuffer(jsonValue))

    if err != nil {
        log.Println("Error on response.\n[ERRO] -", err)
    }

    defer resp.Body.Close()  // must do this

 

    body, _ := ioutil.ReadAll(resp.Body)
	if raw == true {
        log.Println(string([]byte(body)))  // show raw json for debugging
	}

    lookthrustring := string([]byte(body))
	thetoken := gjson.Get(lookthrustring, "token")
	token = thetoken.String()  // store it in the global for other functions to use
	//println(token.String())
	println(token)


}


func post_logout() {
    //
    // gracefully log out of MIP - otherwise need to use MIP client as admin to logout all admins

    url := base_url + "/api/security/logout"

    fmt.Println("Starting the api logout...")

	
    hc := http.Client{}
    req, err := http.NewRequest("POST", url, nil)

    req.Header.Add("Authorization-Token", token)
	req.Header.Add("Accept", "application/json")


    resp, err := hc.Do(req)
    if err != nil {
        // this happens if port is not working, etc

        //       fmt.Printf("The HTTP request failed with error %s\n", err)
        log.Fatal(err)
    }	
	
    defer resp.Body.Close()  // must do this

    fmt.Printf("HTTP: %s\n", resp.Status)

    body, readErr := ioutil.ReadAll(resp.Body)
	if readErr != nil {
		log.Fatal(readErr)
	}

    log.Println(string([]byte(body)))

}


func verifysessionid(onesessionid string, raw bool) (bool, error) {
    // 
    // look for a specific sessionid in the type of payinvoices
	//    which is the session type after checks are cut
	//
	// returns:
	//    true -= if the sessionid was found to exists
	//    error code if failed to function
	//

	var doessessionexist bool  // flag if found or not
	
	
	onetranstype := "payinvoices"  // documents with check info in session of this type after checks cut
	
    //url := base_url + "/api/te/jv/sessions/ids"  // only jv type sessions - confusing docs on type
    //url := base_url + "/api/te/apinvoices/sessions/ids"  // this works to give us api sessions, including API180924-06
    //url := base_url + "/api/te/PayInvoices/sessions/invoices/templates/default" // works giving template
    //url := base_url + "/api/te/PayInvoices/sessions/API180924-06/invoices" //    API180924-06
    url := base_url + "/api/te/" + onetranstype + "/sessions/ids"  // this works to give us api sessions, including API180924-06
 
    if raw == true { 
	    fmt.Println("Get session ids with Token = ", token, " at url = ", url) 
        fmt.Println()
	}


    // Create a new request using http
    req, err := http.NewRequest("GET", url, nil)

    // add authorization header to the req
    req.Header.Add("Authorization-Token", token)
	req.Header.Add("Accept", "application/json")

    // Send req using http Client
    client := &http.Client{}
    resp, err := client.Do(req)
    if err != nil {
        log.Println("Error on response.\n[ERRO] -", err)
		
		return false, err // bail out due to error
		
    }

    body, _ := ioutil.ReadAll(resp.Body)
	if raw == true {
        log.Println(string([]byte(body)))  // show raw json for debugging
	}


	//     number of session ids found
	lookthrustring := string([]byte(body))
	thenumsessionids := gjson.Get(lookthrustring, "#").String() // convert from json to string
    if raw == true { 
    	fmt.Println("thenumsessionids = ", thenumsessionids)
	}

    if onesessionid != "" {
    	// a sessionid was passed on the CL so look in the body to find if it is there
    
	    if raw == true { 	
		
	        fmt.Println("Looking for sessionid = ", onesessionid , "in the list of ", thenumsessionids, " session ids");
	    }
		
	    bodystring := string([]byte(body)) // convert to string for later test
	    teststring := onesessionid // "ASOCKSTEST19072602"


	    if raw == true { 
	        fmt.Println("------")
	    }
		
		
	    if strings.Contains(bodystring, teststring) { 
		    doessessionexist = true // found it
		    if raw == true { 
				fmt.Println("FOUND the sessionid = ",teststring) 
			}
	    } else { 
		    doessessionexist = false // can't find it
		    if raw == true { 
		        fmt.Println(teststring, " is NOT foundz") 
			}
		}
		
		
		return doessessionexist, nil		
		
	} else {
	    // error - no session id given to look for
		oneerror := fmt.Errorf("No sessionid given\n")
		
		return false, oneerror
	}
	
	
}







func fetchsessionidData(w http.ResponseWriter, r *http.Request) {
	// this implements starseeker sending starapi a mipsessionid to then use to fetch data from the MIP API on NetAtWork
	// calls all the above stuff  11-06-2019
	
	var raw bool
	raw = false // true to show more info
	
    vars := mux.Vars(r)
    mipsessionid := vars["mipsessionid"]
    starsystem := vars["starsystem"] // dev OR sys

	//var RecID string // to be found in DB query

	var url string  // initialize for use below when getting session data

	// tilly
    var myMapSlice []M  // uses the type M at top of the program to make array of map full of strings
	onemap := M{} // initialize a map variable so will reuse it in the loops below
	twomap := M{} // initialize a map variable so will reuse it in the loops below

	
	// connect to DB
	//db := dbConn(starsystem)
	
	
    fmt.Println("Endpoint Hit: fetchsessioniddata -- ", mipsessionid, " -- starsystem -- ",starsystem)

	if mipsessionid != "0" && mipsessionid != "" {	
	
		// fetch the token and store into the global variable
		token = "" // clear it
		authenticate_get_token(false, false) // make the first value true to get debug to log
		fmt.Println("~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~")
		fmt.Println("token = ", token )
		
		sessionexists, _ := verifysessionid(mipsessionid, false)   // set the second value to true for raw dump to log
		
		if sessionexists == true {
		
			// found session ok 
			fmt.Println("sessionexists=", sessionexists)	
			
			
			
			
			
			
			onetranstype := "payinvoices"  // documents with check info in session of this type after checks cut


			//////
			////// fetch the documents for the sessionid
			
			
			fmt.Println("passed in a sessionid = ", mipsessionid )
		
		
			url = base_url + "/api/te/" + onetranstype + "/sessions/" + mipsessionid + "/documents" //    API180924-06
		
		
		
			fmt.Println("Show documents for a single sessionid of type = ", onetranstype, " with Token = ", token, " at url = ", url)
			fmt.Println()

			
			// Create a new request using http
			req, err := http.NewRequest("GET", url, nil)

			// add authorization header to the req
			req.Header.Add("Authorization-Token", token)
			req.Header.Add("Accept", "application/json")

			// Send req using http Client
			client := &http.Client{}
			resp, err := client.Do(req)
			if err != nil {
				log.Println("Error on response.\n[ERRO] -", err)
				
				// something to send back to starseeker		
				s := `{"Command":"fetchsessioniddata","Status":"ERRORFETCHINGDOCS"}`
				w.Write([]byte(s))					
				
				post_logout()
			}

			body, _ := ioutil.ReadAll(resp.Body)
			

			log.Println(string([]byte(body)))  // show raw json for debugging
			
			torso = string(body) // this seems to work to send json back down to starseeker
			
			
			// something to send back to starseeker		
			//s := `{"Command":"fetchsessioniddata","Status":"OK"}`
			//w.Write([]byte(s))				
			//w.Write([]byte(torso))		
			//      json.NewEncoder(w).Encode(torso)  // this had worked to send json back


			//     number of documents found
			lookthrustring := string([]byte(body))
			thenumdocuments := gjson.Get(lookthrustring, "TEDOC_DOCNUM.#").String() // convert from json to string
			
			if raw == true {
				fmt.Println("thenumdocuments = ", thenumdocuments)
				fmt.Println()
				fmt.Println()
			}
			
			// convert string to int for looping later
			  numdocuments, err := strconv.Atoi(thenumdocuments)
			  if err != nil {
				// handle error
				fmt.Println(err)
				// os.Exit(2)
				numdocuments = 0 // just to get thru below gracefully
				

				
			  }		
			  
			if raw == true {
				fmt.Println("numdocuments (as int) = ", numdocuments)
			}
			
			if numdocuments > 0 {
			
				// loop thru the documents associated with this session
				// 		

				// initialize a multidimensional slice to hold each actual/check
				// 
				maxnumrows := 25
				transactuals := make([][]string, maxnumrows)
				// Initialize those empty slices
				    maxnumfields := 11
					for s := 0; s < maxnumrows; s++ {
						transactuals[s] = make([]string, maxnumfields)
					}				
				
				
                transcounter := 0  // integer counter for actual transactions
				
				for d := 0; d < numdocuments; d++ {
					if raw == true {
						fmt.Println("d = ", d)
					}
					
					// 
					thedstring := strconv.Itoa(d) // convert from int to string for insertion below
					
					// 
					dTEDOC_DOCNUM := gjson.Get(lookthrustring, "TEDOC_DOCNUM."+thedstring+".TEDOC_DOCNUM").String() // convert from json to string	

					if raw == true {
						fmt.Println("dTEDOC_DOCNUM = ", dTEDOC_DOCNUM)			
					}
					
					dTEDOC_DOCDATE := gjson.Get(lookthrustring, "TEDOC_DOCNUM."+thedstring+".TEDOC_DOCDATE").String() // convert from json to string	

					if raw == true {
						fmt.Println("dTEDOC_DOCDATE = ", dTEDOC_DOCDATE)	
					}

					dTEDOC_DESCRIPTION := gjson.Get(lookthrustring, "TEDOC_DOCNUM."+thedstring+".TEDOC_DESCRIPTION").String() // convert from json to string	

					if raw == true {
						fmt.Println("dTEDOC_DESCRIPTION = ", dTEDOC_DESCRIPTION)		
					}



					///////
					///////
					// take each document and fetch the document details

						url = base_url + "/api/te/" + onetranstype + "/sessions/" + mipsessionid + "/documents/" + dTEDOC_DOCNUM //    
					
					
						if raw == true {
					
							fmt.Println("Show document details for ", dTEDOC_DOCNUM, " with url = ", url)
							fmt.Println()
						}


						// Create a new request using http
						reqdd, errdd := http.NewRequest("GET", url, nil)

						// add authorization header to the req
						reqdd.Header.Add("Authorization-Token", token)
						reqdd.Header.Add("Accept", "application/json")

						// Send reqdd using http Client
						clientdd := &http.Client{}
						respdd, errdd := clientdd.Do(reqdd)
						if errdd != nil {
							log.Println("Error on response.\n[ERRO] -", errdd)
						}

						bodydd, _ := ioutil.ReadAll(respdd.Body)
						if raw == true {
							log.Println(string([]byte(bodydd)))  // show raw json for debugging
						}
						
						
						
						// parse out individual parts of the data we need
						if raw == true {
							println()
							println("PARSING:")
							println()
						}
						
						//     number of transactions (remember there are 2 for every single actual). Multiple actuals COULD be on 1 check
						lookthrustringdd := string([]byte(bodydd))
						thenumtransactions := gjson.Get(lookthrustringdd, "transactions.#").String() // convert from json to string
						
						if raw == true {
							fmt.Println("thenumtransactions = ", thenumtransactions)
						}
						
						// convert string to int for looping later
						  numtransactions, err := strconv.Atoi(thenumtransactions)
						  if err != nil {
							// handle error
							fmt.Println(err)
							// os.Exit(2)
							numtransactions = 0 // just to get thru below gracefully
							
							
						  }		
						  
						if raw == true {
							fmt.Println("numtransactions (as int) = ", numtransactions)
						}
						
						  
						//     number of fields - if document is for multiple checks, the last 7 fields are the total
						thenumfields := gjson.Get(lookthrustringdd, "fields.#")
						
						if raw == true {
							fmt.Println("thenumfields = ", thenumfields)
						}
						
						//     second part of document is called fields and consists of 7 fields of info on payment (one check)
						//         note - I had to use the index number from the parsed json as well
						theTEDOC_SESSION := gjson.Get(lookthrustringdd, "fields.0.TEDOC_SESSION").String()    //  .String()
						
						if raw == true {
							fmt.Println("fields.TEDOC_SESSION    ( session used to make checks ) = ", theTEDOC_SESSION)
						}
						
						theTEDOC_DOCNUM := gjson.Get(lookthrustringdd, "fields.2.TEDOC_DOCNUM").String()
						
						if raw == true {
							fmt.Println("fields.TEDOC_DOCNUM     ( check number ) = ", theTEDOC_DOCNUM)
						}
						
						theTEDOC_PLAYER_ID := gjson.Get(lookthrustringdd, "fields.4.TEDOC_PLAYER_ID").String()
						
						if raw == true {
							fmt.Println("fields.TEDOC_PLAYER_ID  ( Vendor ) = ", theTEDOC_PLAYER_ID)
						}
						
						theTEDOC_DOCDATE := gjson.Get(lookthrustringdd, "fields.5.TEDOC_DOCDATE").String()
						
						if raw == true {
							fmt.Println("fields.TEDOC_DOCDATE    ( Date Paid ) = ", theTEDOC_DOCDATE)
						}
						
					
						theTEDOC_SRC_AMOUNT := gjson.Get(lookthrustringdd, "fields.6.TEDOC_SRC_AMOUNT").String()
						
						if raw == true {
							fmt.Println("fields.TEDOC_SRC_AMOUNT ( Check Amount ) = ", theTEDOC_SRC_AMOUNT)
						}
						
						//token = thetoken.String()  // store it in the global for other functions to use
						//Println(token.String())
						//Println(token)	


						// first part of the document are the transactions - 2 for each actual paid
						// 		
						
						
						
						sumofactualamounts := 0.0 // init double check sum
						for i := 0; i < numtransactions; i++ {
						
							if raw == true {
								fmt.Println("i = ", i)
							}
							
							// 
							theistring := strconv.Itoa(i) // convert from int to string for insertion below
							
							
							// 20510 = accounts payable, 10190 = cash
							theTETRANS_SEGMENT_0 := gjson.Get(lookthrustringdd, "transactions."+theistring+".segments.0.TETRANS_SEGMENT_0").String()
							
							if raw == true {
								fmt.Println("TETRANS_SEGMENT_0 = ", theTETRANS_SEGMENT_0)
							}
							
							// fund
							theTETRANS_SEGMENT_1 := gjson.Get(lookthrustringdd, "transactions."+theistring+".segments.1.TETRANS_SEGMENT_1").String()
							
							if raw == true {
								fmt.Println("TETRANS_SEGMENT_1 ( fund ) = ", theTETRANS_SEGMENT_1)
							}
							
						//loop through fields for transaction info:
							fieldcount, err := strconv.Atoi(gjson.Get(lookthrustringdd, "transactions."+theistring+".fields.#").String())
							
							//fmt.Println("FieldCount:", fieldcount)
								for j := 0; j < fieldcount; j++{
									temp := gjson.Get(lookthrustringdd, "transactions."+theistring+".fields."+strconv.Itoa(j)).String()
									//fmt.Println("TEMP:", temp)

									FieldMap := make(map[string]interface{})

									err := json.Unmarshal([]byte(temp), &FieldMap)
						  
									if err != nil {
											panic(err)
									}
						  
									for key, value := range FieldMap {
									
											if raw == true {
												fmt.Println("index : ", key, " value : ", value)
											}
											
											twomap[key]=value
									}

								}
						//END loop through fields
							//fmt.Println(twomap)

							// effective date
							theTETRANS_EFFECTIVEDATE := twomap["TETRANS_EFFECTIVEDATE"]
							
							if raw == true {
								fmt.Println("TETRANS_EFFECTIVEDATE ( fund ) = ", theTETRANS_EFFECTIVEDATE)
							}
							
							// description
							//theTETRANS_DESCRIPTION := twomap["TETRANS_DESCRIPTION"]
							//fmt.Println("TETRANS_DESCRIPTION ( description ) = ", theTETRANS_DESCRIPTION)
							
							// actual
							theTETRANS_MATCH_DOCNUM := twomap["TETRANS_MATCH_DOCNUM"]
							
							if raw == true {
								fmt.Println("TETRANS_MATCH_DOCNUM ( actual ) = ", theTETRANS_MATCH_DOCNUM)
							}
							
							// dollar amount of actual
							//theTETRANS_SRC_DEBIT := gjson.Get(lookthrustringdd, "transactions."+theistring+".fields.5.TETRANS_SRC_DEBIT").String()
							theTETRANS_SRC_DEBIT, ok := twomap["TETRANS_SRC_DEBIT"].(string)
							
							if raw == true {
								fmt.Println("TETRANS_SRC_DEBIT ( amount of actual ) = ", theTETRANS_SRC_DEBIT)
								fmt.Println(ok)
							}
							
							
							  // convert string to float to double check sum up 
							  amountofactual, err := strconv.ParseFloat(theTETRANS_SRC_DEBIT, 64)
							  if err != nil {
								// handle error
								fmt.Println(err)
								// os.Exit(2)
								amountofactual = 0 // just to get thru gracefully
								
											
								
							  }		
							  sumofactualamounts = sumofactualamounts + amountofactual	
							  if raw == true {
								fmt.Println("sumofactualamounts = ", strconv.FormatFloat(sumofactualamounts, 'f', -1, 64)) 		  
							  }
							  
							// accounting code test ---- only store the credit side of the dual accounting transactions
							if theTETRANS_SEGMENT_0 == "20510" { 
							
								if raw == true {
									fmt.Println("transcounter=", transcounter) // debug
								}
								
								//transactuals[transcounter][1] = "hello"
							
								// first front load the stuff from the above fields before loop
								//transactuals[transcounter][0] = theTEDOC_SESSION    // ( Document session used to make checks )
								//transactuals[transcounter][1] = theTEDOC_DOCNUM     // ( Document check number )
								//transactuals[transcounter][2] = theTEDOC_PLAYER_ID  // ( Document vendor )
								//transactuals[transcounter][3] = theTEDOC_DOCDATE    // ( Document Date Paid )
								//transactuals[transcounter][4] = theTEDOC_SRC_AMOUNT // ( Document Total paid to vendor )
								//transactuals[transcounter][5] = thenumtransactions  // ( Document thenumtransactions )
								// second, load stuff from the details loop
								//transactuals[transcounter][6] = theTETRANS_SEGMENT_0 // ( Transaction Accounting )
								//transactuals[transcounter][7] = theTETRANS_SEGMENT_1 // ( Transaction Fund Code )
								//transactuals[transcounter][8] = theTETRANS_DESCRIPTION // ( Transaction Description )
								//transactuals[transcounter][9] = theTETRANS_MATCH_DOCNUM // ( Transaction Actual ID from ASO )
								//transactuals[transcounter][10] = theTETRANS_SRC_DEBIT // ( Transaction Check Amount )
								
								
								// load up the map and append to the slice
								onemap = M{"docsession": theTEDOC_SESSION,
								"docchecknum": theTEDOC_DOCNUM, 
								"docvendor": theTEDOC_PLAYER_ID, 
								"docdate":theTEDOC_DOCDATE, 
								"docamount": theTEDOC_SRC_AMOUNT, 
								"docnumtrans": thenumtransactions, 
								"transaccounting": theTETRANS_SEGMENT_0, 
								"transfund": theTETRANS_SEGMENT_1, 
								"transdescr": twomap["TETRANS_DESCRIPTION"], 
								"transactual": twomap["TETRANS_MATCH_DOCNUM"], 
								"transamount": twomap["TETRANS_SRC_DEBIT"]}

								myMapSlice = append(myMapSlice, onemap)
							
								transcounter = transcounter + 1 // increment
							}

							  
						} // end of loop
						
						
						
						
						
						//////
						//////




					
					
					} // end of document loop
					
					
				if raw == true {	
					fmt.Println()
					fmt.Println()
					//fmt.Println("transactuals=", transactuals)
					//fmt.Println()					
					//fmt.Println("len of transactuals=", len(transactuals))
					fmt.Println()					
					fmt.Println("numdocuments=", numdocuments)	
					fmt.Println("transcounter=", transcounter)	
					//fmt.Println(reflect.TypeOf(myMapSlice) , " mapslice type before marshall") //reflection to test the type of an object
				}

				//1738
				//myJson, _ := json.MarshalIndent(myMapSlice, "", "    ")
				myJson, _ := json.Marshal(myMapSlice)
				myString := string(myJson)
				
				
				fmt.Println(myString)  // string(myJson))		
					
				if raw == true {
					fmt.Println(reflect.TypeOf(myMapSlice) , " mapslice type") //reflection to test the type of an object
					fmt.Println(reflect.TypeOf(myJson) , " myJson type") //reflection to test the type of an object

					fmt.Println(reflect.TypeOf(myString) , " mystring type") //reflection to test the type of an object
				}
				
				//return sessionexists, numdocuments, transcounter, myString, nil, myMapSlice
				
				
				// something to send back to starseeker		
				//s := `{"Command":"fetchsessioniddata","Status":"OK"}`
				//w.Write([]byte(s))				
				//w.Write([]byte(torso))	
				w.Write(myJson)
				//    json.NewEncoder(w).Encode(myString)  // this had worked to send json back				
				
				
				
				
				
			}

			if raw == true {
				fmt.Println()
				fmt.Println()
			}
			


/// ///

			
			post_logout()
			
			
			
			
		} else {
		
			// did not find the session 
			fmt.Println("sessionexists=", sessionexists)	
		
			// something to send back to starseeker
			s := `{"Command":"fetchsessioniddata","Status":"SESSIONNOTFOUND"}`
			w.Write([]byte(s))			
		
			post_logout()
		
		}
		
	


		
	} else {
		// something to send back
		fmt.Println(" - NOTVALID - mipsessionid = ", mipsessionid)
		
		// something to send back to starseeker		
		s := `{"Command":"fetchsessioniddata","Status":"NEEDSESSIONPARAM"}`
		w.Write([]byte(s))	
		
		post_logout()
		
	}
	// close DB
	//defer db.Close()
	
}

////////////////////////////////////////////////////////////////////////////////////////////////





//func returnSingleArticle(w http.ResponseWriter, r *http.Request) {
//    vars := mux.Vars(r)
 //   key := vars["id"]
//
//    fmt.Println("Endpoint Hit: returnSingleArticle -- ", key)
//
//    for _, article := range Articles {
 //       if article.Id == key {
//            json.NewEncoder(w).Encode(article)
//        } else {
//	    fmt.Println("Endpoint Hit: returnSingleArticle -- No such key = ", key)
//	}
//
//    }
//}

func markrecReconciled(w http.ResponseWriter, r *http.Request) {
	// this implemnets starseeker sending a success so we can mark the recStatus = Reconciled
	
    vars := mux.Vars(r)
    recid := vars["recid"]
    starsystem := vars["starsystem"] // dev OR sys

	var RecID string // to be found in DB query
	var RecStatus string // to be found in DB query
	
	// connect to DB
	db := dbConn(starsystem)
	
	
    fmt.Println("Endpoint Hit: markrecReconciled -- ", recid)

	if recid != "0" && recid != "" {	
	
		// check if in DB
		_ = db.QueryRow("SELECT RecID, RecStatus FROM rec WHERE RecID = ?", recid).Scan(&RecID,&RecStatus)
		//if err != nil {
		//	 panic(err.Error())
		//}
		//fmt.Println(RecID)	
		
		if recid == RecID {
			// found it in DB so mark it
		
		    if RecStatus == "Processing" {
	
				// mark RecStatus in the rec table to Reconciled
				upForm, err := db.Prepare("UPDATE rec SET RecStatus = 'Reconciled'  WHERE RecID = ?")
				if err != nil {
					panic(err.Error())
				}
				upForm.Exec(recid)
				fmt.Println(" - OK - Found the DB record - Updated the RecStatus to Reconciled in rec for RecID = ", recid)

				// something to send back
				s := `{"Command":"success","Status":"OK"}`
				w.Write([]byte(s))	
			
			} else {
			
				// it was not in Processing so don't change it
				fmt.Println(" - WASNOTPROCESSING - Found the DB record for RecID = ", recid, " But RecStatus was wrong = ", RecStatus)
				
				
				// something to send back
				s := `{"Command":"success","Status":"WASNOTPROCESSING"}`
				w.Write([]byte(s))					
				
			}
			
		} else {
			// not found in DB so do nothing but tell caller
			fmt.Println(" - NOTINDB - Cound NOT Find the DB record for recid = ", recid)
			
			s := `{"Command":"success","Status":"NOTINDB"}`
			w.Write([]byte(s))				
		}
	} else {
		// something to send back
		fmt.Println(" - NOTVALID - recid = ", recid)
		
		
		s := `{"Command":"success","Status":"NOTVALID"}`
		w.Write([]byte(s))	
	}
	// close DB
	defer db.Close()
	
}

func markrecError(w http.ResponseWriter, r *http.Request) {
	// this implements starseeker sending a error condition on its end so we can mark the recStatus = Error
	
    vars := mux.Vars(r)
    recid := vars["recid"]
    starsystem := vars["starsystem"] // dev OR sys

	var RecID string // to be found in DB query
	
	// connect to DB
	db := dbConn(starsystem)
	
	
    fmt.Println("Endpoint Hit: markrecError -- ", recid)

	if recid != "0" && recid != "" {	
	
		// check if in DB
		_ = db.QueryRow("SELECT RecID FROM rec WHERE RecID = ?", recid).Scan(&RecID)
		//if err != nil {
		//	 panic(err.Error())
		//}
		//fmt.Println(RecID)	
		
		if recid == RecID {
			// found it in DB so mark it
		
	
			// mark RecStatus in the rec table to Reconciled
			upForm, err := db.Prepare("UPDATE rec SET RecStatus = 'Error'  WHERE RecID = ?")
			if err != nil {
				panic(err.Error())
			}
			upForm.Exec(recid)
			fmt.Println(" - OK - Found the DB record - Updated the RecStatus to Error in rec for RecID = ", recid)

			// something to send back
			s := `{"Command":"recerror","Status":"OK"}`
			w.Write([]byte(s))	
		} else {
			// not found in DB so do nothing but tell caller
			fmt.Println(" - NOTINDB - Cound NOT Find the DB record for recid = ", recid)
			
			s := `{"Command":"recerror","Status":"NOTINDB"}`
			w.Write([]byte(s))				
		}
	} else {
		// something to send back
		fmt.Println(" - NOTVALID - recid = ", recid)
		
		
		s := `{"Command":"recerror","Status":"NOTVALID"}`
		w.Write([]byte(s))	
	}
	// close DB
	defer db.Close()
	
}



func dbConn(starsystem string) (db *sql.DB) {
// passing in the starsystem it will open connection to -  dev OR sys

	if starsystem == "dev" {
	
		// development DB on stardev
		dbDriver := "mysql"
		dbUser := "stardev_cbhc"
		dbPass := "s1t2a3R4!"
		dbName := "stardev_aso"
		db, err := sql.Open(dbDriver, dbUser+":"+dbPass+"@/"+dbName)
		if err != nil {
			panic(err.Error())
		}
		return db
		
	} else if starsystem == "sys" {
	
		// production DB on starsys
		dbDriver := "mysql"
		dbUser := "starsys_cbhc"
		dbPass := "s1t2a3R4!"
		dbName := "starsys_aso"
		db, err := sql.Open(dbDriver, dbUser+":"+dbPass+"@/"+dbName)
		if err != nil {
			panic(err.Error())
		}
		return db	
		
	} else {
	
		// problem here
		fmt.Println(" WARNING - starsystem is not valid: ", starsystem)
		return
		
	}
}




func handleRequests(myport string) {

    fmt.Println("Will listen on port: ",myport)
	
    myRouter := mux.NewRouter().StrictSlash(true)
    myRouter.HandleFunc("/", homePage)
	
	
	//Methods("GET", "OPTIONS") means it support GET, OPTIONS - must do this below
	myRouter.HandleFunc("/healthcheck", healthCheck).Methods("GET")
	
	myRouter.HandleFunc("/askmipver", askmipverCheck).Methods("GET")  // new call to get MIP ver from NetAtWork askmipverCheck
	
    ///myRouter.HandleFunc("/unrec", returnOneUnRecSession).Methods("GET")   // used to test if unreconciled session in DB,
    myRouter.HandleFunc("/unrec/{starsystem}", returnOneUnRecSession).Methods("GET")   // used to test if unreconciled session in DB,
																					 //	if so return json
																					 // added starsystem  to be dev  or prod
																					 //
//    myRouter.HandleFunc("/reset", resetAllRec).Methods("GET")      // used to reset all rec records to 
	                                                               // RecDateTime = NULL for testing use only
	myRouter.HandleFunc("/success/{starsystem}/{recid}", markrecReconciled)  // starseeker can tell starapi it successfully reconciled one recid (session)
	myRouter.HandleFunc("/recerror/{starsystem}/{recid}", markrecError)  // starseeker can tell starapi it failed to reconcil one recid (session)
	
	// following is added before NetAtWork cutover - so starapi is middle man
	myRouter.HandleFunc("/fetchsessioniddata/{starsystem}/{mipsessionid}", fetchsessionidData)  // starseeker asks starapi to fetch session data given one session id passed up to me
	
	colonport := ":"+myport																
    //log.Fatal(http.ListenAndServe(":1962", myRouter))
///////////////////////    log.Fatal(http.ListenAndServe(colonport, myRouter))
	
	
    headersOk := handlers.AllowedHeaders([]string{"Authorization"})
    originsOk := handlers.AllowedOrigins([]string{"*"})
    methodsOk := handlers.AllowedMethods([]string{"GET", "POST", "OPTIONS"})

    fmt.Println("Running server")
	fmt.Println(colonport)
	
    log.Fatal(http.ListenAndServe(colonport, handlers.CORS(originsOk, headersOk, methodsOk)(myRouter)))	
	
	
}

func main() {

	// set the values needed when starapi makes the direct MIP API calls to netAtWork server
		base_url = "http://216.58.162.156:1962" // assign server to call
		//base_url = "https://mipapi.abilaonline.com" // assign server to call


    portPtr := flag.String("port", "1962", "a string")	// -port=9099  api listening on this port
	
	flag.Parse() // uses flag package to allow string, int and boolean flags on CL

	println("----------------------------------------------")
	println("----------------------------------------------")
	println("----------------------------------------------")
	println("----------------------------------------------")
	println("----------------------------------------------")
	println("----------------------------------------------")
	

	
	fmt.Println("Listening on port:", *portPtr)         // show the flag setting


	currentTime := time.Now()	
    fmt.Println("starapi - Rest API Service Started on ", currentTime.Format("2006-01-02 15:04:05"))	


    //s := `{"key1":"0","key2":"0"}`
    //content, _ := json.Marshal(JSONString(s))
    //fmt.Println(string(content))	
	
	
    handleRequests(*portPtr)  // note passing it the port from CL
}
