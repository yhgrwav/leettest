# The README recording: assets/demo.gif. Rebuild — CONTRIBUTING.md, «Docs».
Output assets/demo.gif

Set Shell "bash"
Set Theme "Catppuccin Mocha"
Set FontSize 17
Set Width 1200
Set Height 700
Set Padding 16
Set LetterSpacing 0
Set TypingSpeed 40ms
Set PlaybackSpeed 1.3

# Setup, out of the frame: binaries from bin/; the first-run colour prompt answered with its
# defaults (dark, aurora); the stand answers in 20 ms and freezes for 3 s at the 6th second
# after the first call.
Hide
Type "export PATH=$PWD/bin:$PATH PS1='$ ' && clear" Enter
Type "mkdir -p ~/.config/leettest && printf 'mode: dark\npalette: aurora\n' > ~/.config/leettest/settings.yaml" Enter
Type "stand -delay 20ms -freeze-at 6s -freeze-for 3s -life 2m > /dev/null 2>&1 &" Enter
Type "sleep 1 && clear" Enter
Sleep 2s
Show

Type "leettest -c examples/stand.yaml"
Sleep 400ms
Enter
Sleep 25s
Enter
Sleep 3500ms
