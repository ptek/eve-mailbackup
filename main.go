package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	http "net/http"
	"os"
	"time"

	md "github.com/JohannesKaufmann/html-to-markdown"
)

type Authentication struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
}

type Character struct {
	Id   int    `json:"CharacterID"`
	Name string `json:"CharacterName"`
}

type MailHeader struct {
	Id      int       `json:"mail_id"`
	Subject string    `json:"subject"`
	Time    time.Time `json:"timestamp"`
	From    int       `json:"from"`
}

type Mail struct {
	Header  MailHeader `json:"header,omitempty"`
	Body    string     `json:"body"`
	Subject string     `json:"subject"`
}

func start_callback_server() string {
	server := &http.Server{Addr: ":12525", Handler: nil}
	ctx, cancel := context.WithTimeout(context.Background(), 55*time.Second)
	defer cancel()

	code := make(chan string, 1)

	go func() {
		http.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprintf(w, `
			<!DOCTYPE html>
			<html>
		<head><script>window.close();</script></head>
				<body>Success! You can close this page now.<br/>Your email is being saved as you read this.</body>
			</html>
			`)
			code <- r.URL.Query().Get("code")
		})

		if err := server.ListenAndServe(); err != http.ErrServerClosed {
			log.Fatalf("Authorization http service: %v", err)
		}
	}()

	authCode := <-code
	fmt.Println(authCode)
	server.Shutdown(ctx)
	return authCode
}

func Authenticate(authCode string) Authentication {
	var requestBody = []byte(fmt.Sprintf(`{
		"grant_type": "authorization_code",
		"code": "%s"
	}`, authCode))

	request, _ := http.NewRequest("POST", "https://login.eveonline.com/oauth/token", bytes.NewBuffer(requestBody))
	request.Header.Set("Content-Type", "application/json; charset=UTF-8")
	request.Header.Set("Authorization", "Basic OGRiYjJlMTNiMTNjNDc1ZmIxZTUxMjA3YjVlNzVkZjQ6RmFlOUtsdEcwUFNzcWNmNW45ZkxDUVpWMjVXeGNWMGs1YUh0NmNUMw==")

	response, error := http.DefaultClient.Do(request)
	if error != nil {
		panic(error)
	}
	defer response.Body.Close()

	responseBody, _ := io.ReadAll(response.Body)

	var auth Authentication
	err := json.Unmarshal([]byte(responseBody), &auth)
	if err != nil {
		panic(err)
	}

	return auth
}

func Login() Authentication {
	fmt.Println(`
Welcome to the EVE Online Mail Backup!
This program asks you to authenticate with your character and will save all your mails
into a mail folder. Every eve mail will be in a markdown format.

Please visit the following URL in your browser and authorize the application:`)
	fmt.Println("https://login.eveonline.com/oauth/authorize?response_type=code&redirect_uri=http://localhost:12525/callback&client_id=8dbb2e13b13c475fb1e51207b5e75df4&scope=esi-mail.read_mail.v1")
	authCode := start_callback_server()
	return Authenticate(authCode)
}

func getEsi(auth Authentication, url string) []byte {
	request, _ := http.NewRequest("GET", url, nil)
	request.Header.Set("Authorization", "Bearer "+auth.AccessToken)

	response, error := http.DefaultClient.Do(request)
	if error != nil {
		panic(error)
	}
	defer response.Body.Close()
	responseBody, _ := io.ReadAll(response.Body)
	return responseBody
}

func GetCharacter(auth Authentication) Character {

	response := getEsi(auth, "https://login.eveonline.com/oauth/verify")

	var char Character

	err := json.Unmarshal(response, &char)
	if err != nil {
		panic(err)
	}

	return char
}

func GetMailHeaders(auth Authentication, char Character) []MailHeader {

	var mailHeaders []MailHeader
	var lastMailId int = 0

	for {
		var response []byte
		if lastMailId == 0 {
			response = getEsi(auth, fmt.Sprintf("https://esi.evetech.net/latest/characters/%d/mail/", char.Id))
		} else {
			response = getEsi(auth, fmt.Sprintf("https://esi.evetech.net/latest/characters/%d/mail/?last_mail_id=%d", char.Id, lastMailId))
		}

		var newMailHeaders []MailHeader

		err := json.Unmarshal(response, &newMailHeaders)
		if err != nil {
			fmt.Println(string(response))
			panic(err)
		}

		if len(newMailHeaders) == 0 {
			return mailHeaders
		}

		mailHeaders = append(mailHeaders, newMailHeaders...)

		// get the lowest mail id to paginate to the next request
		lastMailId = newMailHeaders[0].Id
		for _, v := range newMailHeaders {
			if v.Id < lastMailId {
				lastMailId = v.Id
			}
		}
	}
}

func GetMail(auth Authentication, char Character, mailHeader MailHeader) Mail {
	response := getEsi(auth, fmt.Sprintf("https://esi.evetech.net/latest/characters/%d/mail/%d/", char.Id, mailHeader.Id))

	var mail Mail
	err := json.Unmarshal(response, &mail)
	if err != nil {
		fmt.Println(string(response))
		panic(err)
	}
	mail.Header = mailHeader
	return mail
}

func SaveMail(mail Mail) {
	file, err := os.Create(fmt.Sprintf("mail/%v %d.txt", mail.Header.Time.Format(time.RFC3339), mail.Header.Id))
	if err != nil {
		panic(err)
	}
	defer file.Close()

	converter := md.NewConverter("", true, nil)

	markdownBody, err := converter.ConvertString(mail.Body)
	if err != nil {
		log.Fatal(err)
	}

	file.WriteString(mail.Subject)
	file.WriteString("\n")
	file.WriteString(fmt.Sprintf("%d", mail.Header.From))
	file.WriteString("\n")
	file.WriteString(fmt.Sprintf("%v", mail.Header.Time))
	file.WriteString("\n---\n\n")
	file.WriteString(markdownBody)
}

func main() {
	auth := Login()
	char := GetCharacter(auth)
	mailHeaders := GetMailHeaders(auth, char)

	//make directory to save mail
	err := os.MkdirAll("eve-mail", os.ModePerm)
	if err != nil {
		panic(err)
	}

	for _, mailHeader := range mailHeaders {
		fmt.Println(mailHeader.Subject)
		mail := GetMail(auth, char, mailHeader)
		SaveMail(mail)
	}
}
