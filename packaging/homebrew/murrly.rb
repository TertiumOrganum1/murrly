# Homebrew formula for Murrly.
#
# To distribute via Homebrew, create a tap repo named "homebrew-murrly"
# on GitHub and copy this file to "Formula/murrly.rb" in that repo.
# Then users install with:
#
#   brew install tertiumorganum1/murrly/murrly
#
# Update the `url`, `sha256`, and `version` block on each tagged release.

class Murrly < Formula
  desc "Local push-to-talk voice-to-text dictation (Whisper + Metal/CUDA)"
  homepage "https://github.com/tertiumorganum1/murrly"
  url "https://github.com/tertiumorganum1/murrly/archive/refs/tags/v0.2.0.tar.gz"
  sha256 "REPLACE_WITH_TARBALL_SHA256"
  license "MIT"
  head "https://github.com/tertiumorganum1/murrly.git", branch: "master"

  depends_on "cmake" => :build
  depends_on "go" => :build
  depends_on "librsvg" => :build
  depends_on "pkg-config" => :build
  depends_on "portaudio"

  on_linux do
    depends_on "xclip"
    depends_on "xdotool"
  end

  def install
    # Clone whisper.cpp shallowly and build it with the right backend.
    system "scripts/ensure-whisper-cpp.sh"
    system "make", "whisper"

    # Build the Go binary (mk/{darwin,linux}.mk sets the right CGO flags).
    system "make", "build"
    bin.install "bin/murrly"

    # On macOS, also assemble the .app bundle so the brand-icon appears
    # in the Dock and the menu-bar overlay behaves like a real app.
    if OS.mac?
      system "make", "icons"
      # install-mac.sh copies to /Applications; let it.
      system "scripts/install-mac.sh"
    else
      # On Linux: place the colored cat icon for the .desktop launcher.
      (share/"icons/hicolor/512x512/apps").install "assets/icons/masters/app_icon_master_1024.png" => "murrly.png"
      (share/"applications").write desktop_file
    end
  end

  def desktop_file
    <<~EOS
      [Desktop Entry]
      Type=Application
      Name=Murrly
      Comment=Local push-to-talk voice-to-text dictation
      Exec=#{bin}/murrly
      Icon=murrly
      Terminal=false
      Categories=Utility;Accessibility;
    EOS
  end

  def caveats
    on_macos do
      <<~EOS
        Murrly is now in /Applications/Murrly.app and on your PATH as `murrly`.

        First launch:
          1. Right-click /Applications/Murrly.app → Open (one-time Gatekeeper bypass).
          2. macOS will prompt for Microphone access — grant.
          3. macOS will prompt for Accessibility — open System Settings, toggle
             Murrly on, then relaunch.

        Then hold the configured hotkey (default F12) in any text field
        and dictate. The translucent pill at the top of your screen shows
        the current state.

        Config: ~/Library/Application Support/Murrly/config.toml
        Models: ~/Library/Application Support/Murrly/models/
        Log:    ~/Library/Caches/Murrly/Murrly.log
      EOS
    end
    on_linux do
      <<~EOS
        Launch from your application menu (Murrly) or run `murrly`.
        Hold F12 in any text field, speak, release — text pastes via xdotool.

        Config: ~/.config/murrly/config.toml
        Models: ~/.local/share/murrly/models/
        Log:    ~/.cache/murrly/murrly.log

        First run downloads the Whisper model (~547MB-3GB). Subsequent
        launches use the cached file.
      EOS
    end
  end

  test do
    # The binary has no --version flag yet; just confirm it's executable
    # and Linux X11 / macOS Cocoa link succeeded.
    system "file", "#{bin}/murrly"
  end
end
