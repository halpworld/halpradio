# typed: false
# frozen_string_literal: true

class Halpradio < Formula
  desc "LazyVim-inspired Terminal Internet Radio Streamer"
  homepage "https://github.com/halpworld/halpradio"
  version "0.0.2"
  license "MIT"

  on_macos do
    if Hardware::CPU.arm?
      url "https://github.com/halpworld/halpradio/releases/download/v0.0.2/halpradio_0.0.2_darwin_arm64.tar.gz"
      sha256 "e7d344645a807558f10125b9da2087acc14e09682b0837f197201d3e62682c0f"
    else
      url "https://github.com/halpworld/halpradio/releases/download/v0.0.2/halpradio_0.0.2_darwin_amd64.tar.gz"
      sha256 "70f89615278eec4105460cc1ea6a2adb80975050baf2a287c4b534af58271e88"
    end
  end

  on_linux do
    if Hardware::CPU.arm? && Hardware::CPU.is_64_bit?
      url "https://github.com/halpworld/halpradio/releases/download/v0.0.2/halpradio_0.0.2_linux_arm64.tar.gz"
      sha256 "022baff84f0e21a985690617938ec707f02938bd00a0505f46f000bdd2f96c49"
    else
      url "https://github.com/halpworld/halpradio/releases/download/v0.0.2/halpradio_0.0.2_linux_amd64.tar.gz"
      sha256 "3c1aaae06b31394e5c4a2c538af0ee40981ee5b1047adc1ad015e323d4ad1a33"
    end
  end

  def install
    bin.install "halpradio"
  end

  test do
    assert_match "halpradio", shell_output("#{bin}/halpradio -version")
  end
end
