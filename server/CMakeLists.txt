add_executable(gated-server
 main.cpp
)

CPMAddPackage(
  URI "gh:CrowCpp/Crow@1.3.3"
  OPTIONS 
  "CROW_BUILD_EXAMPLES OFF"
  "CROW_BUILD_TOOLS OFF"
  "CROW_BUILD_TESTS OFF"
  "CROW_BUILD_DOCS OFF"
  "CROW_ENABLE_COMPRESSION ON")


target_precompile_headers(gated-server
  PRIVATE
  <crow.h>)

target_link_libraries(gated-server Crow::Crow)

install(TARGETS gated-server DESTINATION bin)
