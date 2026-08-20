#include <cstdint>

#include "crow.h"

struct QueuedBitstream {
  std::string filename;
  std::vector<uint8_t> file_container;

  crow::json::wvalue get_status() {
    crow::json::wvalue status;
    status["filename"] = filename;
    return status;
  }
};

int main(int argc, char* argv[]) {
  crow::SimpleApp app;
  crow::mustache::set_base(".");

  CROW_ROUTE(app, "/upload_bitstream")([](const crow::request& req) {
    QueuedBitstream output_object;

    crow::multipart::message_view file_message(req);
    for (const auto& part : file_message.part_map) {
      const auto& part_name = part.first;
      const auto& part_value = part.second;
      CROW_LOG_DEBUG << "Part: " << part_name;
      if ("InputFile" == part_name) {
        auto headers_it = part_value.headers.find("Content-Disposition");
        if (headers_it == part_value.headers.end()) {
          CROW_LOG_ERROR << "No Content-Disposition found";
          return crow::response(400);
        }
        auto params_it = headers_it->second.params.find("filename");
        if (params_it == headers_it->second.params.end()) {
          CROW_LOG_ERROR << "Part with name \"InputFile\" should have a file";
          return crow::response(400);
        }
        const std::string outfile_name{params_it->second};

        for (const auto& part_header : part_value.headers) {
          const auto& part_header_name = part_header.first;
          const auto& part_header_val = part_header.second;
          CROW_LOG_DEBUG << "Header: " << part_header_name << '='
                         << part_header_val.value;
          for (const auto& param : part_header_val.params) {
            const auto& param_key = param.first;
            const auto& param_val = param.second;
            CROW_LOG_DEBUG << " Param: " << param_key << ',' << param_val;
          }
        }

        output_object.file_container.insert(output_object.file_container.end(),
                                            part_value.body.begin(),
                                            part_value.body.end());
        output_object.filename = outfile_name;
      } else {
        CROW_LOG_DEBUG << " Value: " << part_value.body << '\n';
      }
    }
    return crow::response(200, output_object.get_status());
  });

  app.loglevel(crow::LogLevel::Debug);
  app.port(8080).run();

  return 0;
}
