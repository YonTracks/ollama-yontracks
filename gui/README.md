# Ollama App

## Linux

TODO

## MacOS

TODO

## Windows
```
ollama-yontracks/
├── app/
│   ├── main.go
│   ├── lifecycle/
│   │   ├── gui_windows.go
│   │   ├── lifecycle.go
├── gui/
│   ├── main.go   
│   ├── ui.go 
│   ├── ollama.go
│   ├── db.go 
│   ├── ollama_chats.db          
│   └── DemoGUI.exe
``` 

## DB
`ollama_chats.db` is a SQLite database that stores chat history. It is used by the GUI to display past conversations. 
## NOTICE!
be sure to backup `ollama_chats.db` before uninstalling or updating the app.  
`ollama_chats.db` will not persist across app rebuilds / reinstalls, so it is recommended to back up the database before uninstalling or updating the app. 

- The location of `ollama_chats.db` on Windows is typically:

`C:/Users/<username>/AppData/Local/Programs/Ollama/ollama_chats.db`
 
## Build
In the top directory of this repo, run:

!! !! If `DemoGUI.exe` is running when you run this command, a corrupted executable file will be created and need to be deleted the app may not function properly. !!
```
go build -o DemoGUI.exe
```

 To bypass OllamaSetup.exe, build and run the app directly, testing the tray menu functionality and any app changes, use:

add your username to the path below. Replace `<username>` with your actual username.
!! If DemoGUI.exe exists in the AppData directory, it will be overwritten by this command. !!
!! If DemoGUI.exe is running when you run this command, a corrupted executable file will be created and need to be deleted the app may not function properly. !!


```
go build -o C:/Users/<username>/AppData/Local/Programs/Ollama/DemoGUI.exe ./gui
```
