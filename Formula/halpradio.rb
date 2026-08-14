# typed: false
# frozen_string_literal: true

class Halpradio < Formula
  desc "LazyVim-inspired Terminal Internet Radio Streamer"
  homepage "https://github.com/halpworld/halpradio"
  version "0.0.3"
  license "MIT"

  on_macos do
    if Hardware::CPU.arm?
      url "https://github.com/halpworld/halpradio/releases/download/v0.0.3/halpradio_0.0.3_darwin_arm64.tar.gz"
    else
      url "https://github.com/halpworld/halpradio/releases/download/v0.0.3/halpradio_0.0.3_darwin_amd64.tar.gz"
    end
  end

  on_linux do
    if Hardware::CPU.arm? && Hardware::CPU.is_64_bit?
      url "https://github.com/halpworld/halpradio/releases/download/v0.0.3/halpradio_0.0.3_linux_arm64.tar.gz"
    else
      url "https://github.com/halpworld/halpradio/releases/download/v0.0.3/halpradio_0.0.3_linux_amd64.tar.gz"
    end
  end

  def install
    bin.install "halpradio"
  end

  test do
    assert_match "halpradio", shell_output("#{bin}/halpradio -version")
  end
end
