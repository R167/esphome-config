// UDP Registry Client for ESPHome
// Ultra-lightweight service registry client using UDP
// Minimal memory footprint for ESP8266 devices

#include "esphome.h"

class UDPRegistryClient : public Component {
 public:
  void setup() override {
    // Register on startup after WiFi is connected
    set_timeout("initial_register", 10000, [this]() {
      this->register_device();
    });
    
    // Set up periodic registration
    set_interval("periodic_register", interval_ms_, [this]() {
      this->register_device();
    });
  }

  void set_registry_host(const std::string &host) {
    registry_host_ = host;
  }
  
  void set_registry_port(uint16_t port) {
    registry_port_ = port;
  }
  
  void set_device_name(const std::string &name) {
    device_name_ = name;
  }
  
  void set_device_labels(const std::string &labels) {
    device_labels_ = labels;
  }
  
  void set_interval_ms(uint32_t interval_ms) {
    interval_ms_ = interval_ms;
  }

 private:
  void register_device() {
    if (!WiFi.isConnected()) {
      ESP_LOGW("udp_registry", "WiFi not connected, skipping registration");
      return;
    }

    WiFiUDP udp;
    if (!udp.begin(0)) {  // Use random local port
      ESP_LOGE("udp_registry", "Failed to start UDP client");
      return;
    }

    // Build registration packet: "REGISTER|host|name|labels"
    std::string host = device_name_ + ".local:80";
    std::string packet = "REGISTER|" + host + "|" + device_name_ + "|" + device_labels_;
    
    ESP_LOGD("udp_registry", "Sending registration: %s", packet.c_str());
    
    // Send UDP packet
    if (udp.beginPacket(registry_host_.c_str(), registry_port_)) {
      udp.write(reinterpret_cast<const uint8_t*>(packet.c_str()), packet.length());
      if (udp.endPacket()) {
        ESP_LOGI("udp_registry", "Registration sent successfully to %s:%d", 
                 registry_host_.c_str(), registry_port_);
      } else {
        ESP_LOGW("udp_registry", "Failed to send registration packet");
      }
    } else {
      ESP_LOGW("udp_registry", "Failed to begin UDP packet to %s:%d", 
               registry_host_.c_str(), registry_port_);
    }
    
    udp.stop();
  }

  std::string registry_host_ = "192.168.1.10";
  uint16_t registry_port_ = 8081;
  std::string device_name_ = "esp-device";
  std::string device_labels_ = "type:sensor";
  uint32_t interval_ms_ = 30000;  // Default 30 seconds
};