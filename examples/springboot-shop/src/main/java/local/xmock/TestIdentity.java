package local.xmock;
import jakarta.servlet.http.HttpServletRequest;
import org.springframework.context.annotation.Profile;
import org.springframework.http.HttpStatus;
import org.springframework.stereotype.Component;
import org.springframework.web.server.ResponseStatusException;
@Component @Profile("mock")
public class TestIdentity {
 public long user(HttpServletRequest request){String value=request.getHeader("X-Test-User-Id");if("2001".equals(value))return 2001;if("2002".equals(value))return 2002;throw new ResponseStatusException(HttpStatus.UNAUTHORIZED,"test identity required");}
}
