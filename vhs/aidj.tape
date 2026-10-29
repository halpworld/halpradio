# VHS Tape for halpradio Local AI Radio DJ & Interactive Liner Notes Companion
# Run: vhs vhs/aidj.tape

Output docs/images/aidj.gif

Set Shell "bash"
Set FontSize 16
Set Width 1240
Set Height 820
Set Padding 20
Set Theme "TokyoNight"
Set WindowBar "Colorful"
Set Framerate 24

# Launch halpradio with AI DJ enabled
Type "./halpradio --ai-dj -backend native -discord=false -notifications=false -mpris=false -ipc=false"
Enter
Sleep 2s

# Move into station list and start playback
Type "l"
Sleep 500ms
Type "j"
Sleep 400ms
Enter
Sleep 2s

# Open Interactive Liner Notes & Artist Biography Drawer ('i')
Type "i"
Sleep 2.5s

# Scroll through artist biography, discography & trivia
Type "j"
Sleep 500ms
Type "j"
Sleep 500ms
Type "k"
Sleep 600ms

# Trigger on-demand Local AI Radio DJ track intro ('d') with audio ducking
Type "d"
Sleep 3s

# Open AI DJ Studio & ONNX Voice Manager modal ('D')
Type "D"
Sleep 2s

# Cycle DJ Persona and TTS Engine inside AI DJ Studio
Type "j"
Sleep 500ms
Enter
Sleep 800ms
Type "j"
Sleep 500ms
Enter
Sleep 800ms
Type "j"
Sleep 800ms

# Close AI DJ Studio modal and Liner Notes drawer
Escape
Sleep 800ms
Type "i"
Sleep 1s

# Quit cleanly
Type "q"
Sleep 500ms
